package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"omnihub/internal/auth"
)

type phoneThumbInput struct {
	Device   string `json:"device"`
	User     string `json:"user"`
	Password string `json:"password"`
	Port     int    `json:"port"`
}
type phoneThumbSummary struct {
	Token       string `json:"token"`
	Device      string `json:"device"`
	Directories int    `json:"directories"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
	Skipped     int    `json:"skipped"`
	Trash       string `json:"trash"`
	Moved       bool   `json:"moved"`
	Remaining   int    `json:"remaining"`
	CanClean    bool   `json:"can_clean"`
}
type phoneThumbDir struct {
	Rel, Signature string
	Trash          string
	Files          int
	Bytes          int64
	Moved          bool
}
type phoneThumbReview struct {
	Summary phoneThumbSummary
	Dirs    []phoneThumbDir
	Input   phoneThumbInput // Password is cleared immediately after login; never serialized.
	Base    string
	Library string
	Client  *http.Client
	Forward string
	Expires time.Time
}
type adbDevice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (r *Router) adbRead(ctx context.Context, device string, args ...string) ([]byte, error) {
	if r.phoneADB == "" {
		return nil, errors.New("未找到 adb（Android platform-tools）")
	}
	if len(device) > 256 || strings.ContainsAny(device, "\x00\r\n") {
		return nil, errors.New("设备标识无效")
	}
	if device != "" {
		args = append([]string{"-s", device}, args...)
	}
	cmd := exec.CommandContext(ctx, r.phoneADB, args...)
	var errout bytes.Buffer
	cmd.Stderr = &errout
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	out, readErr := io.ReadAll(io.LimitReader(pipe, (32<<20)+1))
	if len(out) > 32<<20 {
		_ = cmd.Process.Kill()
		readErr = errors.New("手机清单超过 32 MiB")
	}
	err = errors.Join(readErr, cmd.Wait())
	if err != nil {
		return out, fmt.Errorf("ADB 操作失败：%w %s", err, strings.TrimSpace(errout.String()))
	}
	return bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n")), nil
}

func (r *Router) handlePhoneThumbs(w http.ResponseWriter, req *http.Request, action string) {
	if auth.IsWANFromContext(req.Context()) || auth.UserFromContext(req.Context()).Role != auth.RoleAdmin {
		http.Error(w, "仅本机或局域网管理员可维护手机", 403)
		return
	}
	if action == "phone-devices" && req.Method == http.MethodGet {
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		out, err := r.adbRead(ctx, "", "devices", "-l")
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		devices := []adbDevice{}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[1] == "device" {
				devices = append(devices, adbDevice{f[0], strings.Join(f[2:], " ") + " · " + f[0]})
			}
		}
		writeJSON(w, 200, devices)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if action == "phone-thumbs-history" {
		var in phoneThumbInput
		if json.NewDecoder(req.Body).Decode(&in) != nil || in.Device == "" || len(in.Device) > 256 {
			http.Error(w, "请选择已授权的手机", 400)
			return
		}
		select {
		case r.svJobs.requests <- "phone-thumbs-history\x00" + in.Device:
		default:
			http.Error(w, "已有任务排队", 429)
			return
		}
		writeJSON(w, 202, r.svJobs.status())
		return
	}
	if action == "phone-thumbs-plan" {
		var in phoneThumbInput
		if json.NewDecoder(req.Body).Decode(&in) != nil || in.Device == "" || len(in.Device) > 256 || len(in.User) > 128 || len(in.Password) > 1024 {
			http.Error(w, "请选择已授权的手机并填写手机 omniHub 管理员账号", 400)
			return
		}
		if in.Port == 0 {
			in.Port = 8998
		}
		if in.Port < 1 || in.Port > 65535 {
			http.Error(w, "手机服务端口无效", 400)
			return
		}
		data, _ := json.Marshal(in)
		select {
		case r.svJobs.requests <- "phone-thumbs-plan\x00" + string(data):
		default:
			http.Error(w, "已有任务排队", 429)
			return
		}
	} else if action == "phone-thumbs-clean" || action == "phone-thumbs-restore" {
		var in struct {
			Token string `json:"token"`
		}
		if json.NewDecoder(req.Body).Decode(&in) != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		r.svJobs.mu.Lock()
		p := r.svJobs.phoneReview
		valid := p != nil && p.Summary.Token == in.Token && (action == "phone-thumbs-restore" || time.Now().Before(p.Expires))
		r.svJobs.mu.Unlock()
		if !valid {
			http.Error(w, "预览已过期，请重新核对手机封面", 409)
			return
		}
		select {
		case r.svJobs.requests <- action + "\x00" + in.Token:
		default:
			http.Error(w, "已有任务排队", 429)
			return
		}
	} else {
		http.NotFound(w, req)
		return
	}
	writeJSON(w, 202, r.svJobs.status())
}

func (p *phoneThumbReview) request(ctx context.Context, method, route string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.Base+route, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := p.Client
	if route == "/api/media/shortvideo/admin/status" {
		copy := *client
		copy.Timeout = 2 * time.Minute
		client = &copy
	}
	return client.Do(req)
}

func (r *Router) closePhoneForward(p *phoneThumbReview) {
	if p == nil || p.Forward == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.Client != nil {
		if res, err := p.request(ctx, http.MethodPost, "/api/auth/logout", strings.NewReader("{}")); err == nil {
			res.Body.Close()
		}
	}
	_, _ = r.adbRead(ctx, p.Input.Device, "forward", "--remove", p.Forward)
}

func (r *Router) planPhoneThumbs(body string) {
	w := r.svJobs
	ctx := w.begin("phone-thumbs-plan", 0)
	var in phoneThumbInput
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		w.finish("phone-thumbs-plan", "", err)
		return
	}
	w.note("连接已授权 ADB 设备：" + in.Device)
	p := &phoneThumbReview{Input: in, Expires: time.Now().Add(30 * time.Minute)}
	completed := false
	defer func() {
		if !completed {
			r.closePhoneForward(p)
		}
	}()
	out, err := r.adbRead(ctx, in.Device, "forward", "tcp:0", fmt.Sprintf("tcp:%d", in.Port))
	if err != nil {
		w.finish("phone-thumbs-plan", "", err)
		return
	}
	port, e := strconv.Atoi(strings.TrimSpace(string(out)))
	if e != nil || port < 1 {
		w.finish("phone-thumbs-plan", "", errors.New("ADB 没有返回转发端口"))
		return
	}
	p.Forward = fmt.Sprintf("tcp:%d", port)
	p.Base = fmt.Sprintf("http://127.0.0.1:%d", port)
	jar, _ := cookiejar.New(nil)
	p.Client = &http.Client{Timeout: 20 * time.Second, Jar: jar}
	login, _ := json.Marshal(map[string]string{"username": in.User, "password": in.Password})
	res, err := p.request(ctx, http.MethodPost, "/api/auth/login", bytes.NewReader(login))
	p.Input.Password = ""
	if err != nil {
		w.finish("phone-thumbs-plan", "", err)
		return
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		w.finish("phone-thumbs-plan", "", errors.New("手机 omniHub 登录失败，请检查手机管理员账号和密码"))
		return
	}
	w.note("已登录手机 omniHub，核对资源库及缩略图进度")
	marker, err := r.adbRead(ctx, in.Device, "shell", "cat "+shellQuote(path.Join(path.Dir(path.Dir(r.phoneDir)), "omnilibrary.json")))
	var m struct {
		ID string `json:"id"`
	}
	if err != nil || json.Unmarshal(marker, &m) != nil || m.ID == "" {
		w.finish("phone-thumbs-plan", "", errors.New("无法核对手机资源库身份"))
		return
	}
	p.Library = m.ID
	if err = r.phoneThumbReady(ctx, p); err != nil {
		w.finish("phone-thumbs-plan", "", err)
		return
	}
	w.note("手机缩略图处理已结束；读取旧 .thumbs 目录，逐项核对数据库封面")
	inventoryScript := "slot=$(printf '\\173\\175'); for d do printf '__OMNI_DIR__%s\\n' \"$d\"; (cd \"$d\" && find . -mindepth 1 -maxdepth 1 -exec stat -c '%f|%n|%s|%Y' \"$slot\" + | sort) || exit 1; done"
	listing, err := r.adbRead(ctx, in.Device, "shell", "cd "+shellQuote(r.phoneDir)+" && find . -mindepth 3 -maxdepth 3 -type d -name .thumbs -exec sh -c "+shellQuote(inventoryScript)+" sh {} + && echo __OMNI_LIST_END__")
	if err != nil {
		w.finish("phone-thumbs-plan", "", err)
		return
	}
	if !strings.HasSuffix(string(listing), "__OMNI_LIST_END__\n") {
		w.finish("phone-thumbs-plan", "", errors.New("手机目录清单不完整"))
		return
	}
	type inventory struct {
		dir  string
		data []byte
	}
	var dirs []inventory
	var current string
	var rows strings.Builder
	flush := func() {
		if current != "" {
			dirs = append(dirs, inventory{current, []byte(rows.String())})
			rows.Reset()
		}
	}
	for _, line := range strings.Split(string(listing), "\n") {
		if d, ok := strings.CutPrefix(line, "__OMNI_DIR__"); ok {
			flush()
			current = strings.TrimPrefix(d, "./")
		} else if line != "" && line != "__OMNI_LIST_END__" {
			rows.WriteString(line)
			rows.WriteByte('\n')
		}
	}
	flush()
	w.mu.Lock()
	w.total = len(dirs)
	w.mu.Unlock()
	for _, item := range dirs {
		dir := item.dir
		if path.Clean(dir) != dir || strings.Count(dir, "/") != 2 || path.Base(dir) != ".thumbs" || strings.ContainsAny(dir, "\t\r\n") {
			w.finish("phone-thumbs-plan", "", errors.New("旧封面目录路径无效"))
			return
		}
		entries, sig, bytes, err := parsePhoneThumbFiles(item.data)
		if err != nil {
			w.finish("phone-thumbs-plan", "", err)
			return
		}
		valid := len(entries) > 0
		if valid {
			valid, err = r.checkPhoneCovers(ctx, p, dir, entries)
		}
		if err != nil {
			w.finish("phone-thumbs-plan", "", err)
			return
		}
		if valid {
			p.Dirs = append(p.Dirs, phoneThumbDir{Rel: dir, Signature: sig, Files: len(entries), Bytes: bytes})
			p.Summary.Files += len(entries)
			p.Summary.Bytes += bytes
			w.note(fmt.Sprintf("核对通过：%s · %d 张", dir, len(entries)))
		} else {
			p.Summary.Skipped++
			w.note("保留：" + dir + "（存在未入库、无权限或无法读取的封面）")
		}
		w.progress(0)
	}
	id := sha256.Sum256([]byte(in.Device + time.Now().String()))
	p.Summary.Token = hex.EncodeToString(id[:16])
	p.Summary.Device = in.Device
	p.Summary.Directories = len(p.Dirs)
	p.Summary.Remaining = len(p.Dirs)
	p.Summary.CanClean = true
	p.Summary.Trash = path.Join(path.Dir(path.Dir(r.phoneDir)), ".omni_data", "trash", "shortvideo-thumbnails", time.Now().Format("20060102-150405"))
	w.mu.Lock()
	old := w.phoneReview
	w.phoneReview = p
	w.mu.Unlock()
	r.closePhoneForward(old)
	completed = true
	w.finish("phone-thumbs-plan", fmt.Sprintf("可回收 %d 个旧封面目录，%d 张（%.2f GB）；保留 %d 个目录", len(p.Dirs), p.Summary.Files, float64(p.Summary.Bytes)/1e9, p.Summary.Skipped), nil)
}

func (r *Router) phoneThumbReady(ctx context.Context, p *phoneThumbReview) error {
	res, err := p.request(ctx, http.MethodGet, "/api/media/shortvideo/admin/status", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("需要手机 omniHub 管理员权限")
	}
	var s struct {
		Worker struct {
			Job string `json:"job"`
		} `json:"worker"`
		Libraries []struct {
			ID    string `json:"library_id"`
			Stats struct {
				Videos, Albums, Thumbnails int
				Failures                   int `json:"thumbnail_failures"`
			} `json:"stats"`
		} `json:"libraries"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&s); err != nil {
		return err
	}
	if s.Worker.Job != "" {
		return errors.New("手机正在处理短视频，请空闲后再核对")
	}
	for _, lib := range s.Libraries {
		if lib.ID == p.Library {
			if lib.Stats.Thumbnails+lib.Stats.Failures < lib.Stats.Videos+lib.Stats.Albums {
				return errors.New("手机缩略图尚未处理完，不清理旧封面")
			}
			return nil
		}
	}
	return errors.New("手机服务未挂载这个资源库")
}

func (r *Router) phoneThumbFiles(ctx context.Context, p *phoneThumbReview, dir string) ([]string, string, int64, error) {
	out, err := r.adbRead(ctx, p.Input.Device, "shell", "cd "+shellQuote(path.Join(r.phoneDir, dir))+" && find . -mindepth 1 -maxdepth 1 -exec stat -c '%f|%n|%s|%Y' {} + | sort")
	if err != nil {
		return nil, "", 0, err
	}
	return parsePhoneThumbFiles(out)
}

func parsePhoneThumbFiles(out []byte) ([]string, string, int64, error) {
	var names []string
	var size int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) != 4 {
			return nil, "", 0, nil
		}
		mode, modeErr := strconv.ParseUint(f[0], 16, 32)
		f[1] = strings.TrimPrefix(f[1], "./")
		if modeErr != nil || mode&0170000 != 0100000 || !strings.HasSuffix(f[1], ".webp") || path.Base(f[1]) != f[1] {
			return nil, "", 0, nil
		}
		n, e := strconv.ParseInt(f[2], 10, 64)
		if e != nil || n < 0 {
			return nil, "", 0, errors.New("旧封面文件清单无效")
		}
		names = append(names, f[1])
		size += n
	}
	h := sha256.Sum256(out)
	return names, hex.EncodeToString(h[:]), size, nil
}

func (r *Router) checkPhoneCovers(ctx context.Context, p *phoneThumbReview, dir string, names []string) (bool, error) {
	// Pipeline small HEAD batches over one authenticated loopback-forwarded
	// connection. Every image is still checked; only network round trips change.
	base, err := url.Parse(p.Base)
	if err != nil {
		return false, err
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", base.Host)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriterSize(conn, 64<<10)
	for start := 0; start < len(names); start += 32 {
		conn.SetDeadline(time.Now().Add(45 * time.Second))
		requests := make([]*http.Request, 0, 32)
		for _, name := range names[start:min(start+32, len(names))] {
			id := path.Join(path.Dir(dir), strings.TrimSuffix(name, ".webp"))
			uri := p.Base + "/api/media/shortvideo/thumbnail?" + url.Values{"library_id": {p.Library}, "id": {id}}.Encode()
			req, e := http.NewRequestWithContext(ctx, http.MethodHead, uri, nil)
			if e != nil {
				return false, e
			}
			if p.Client.Jar != nil {
				for _, cookie := range p.Client.Jar.Cookies(base) {
					req.AddCookie(cookie)
				}
			}
			if e = req.Write(writer); e != nil {
				return false, e
			}
			requests = append(requests, req)
		}
		if err = writer.Flush(); err != nil {
			return false, err
		}
		valid := true
		for _, req := range requests {
			res, e := http.ReadResponse(reader, req)
			if e != nil {
				return false, e
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				valid = false
			}
		}
		if !valid {
			return false, nil
		}
	}
	// Read one real JPEG per directory as well as checking every entry's presence.
	u := "/api/media/shortvideo/thumbnail?" + url.Values{"library_id": {p.Library}, "id": {path.Join(path.Dir(dir), strings.TrimSuffix(names[0], ".webp"))}}.Encode()
	res, err := p.request(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	cfg, _, err := image.DecodeConfig(io.LimitReader(res.Body, 4<<20))
	return err == nil && cfg.Width > 0 && cfg.Height > 0, nil
}

func (r *Router) movePhoneThumbs(restore bool) {
	w := r.svJobs
	job := "phone-thumbs-clean"
	if restore {
		job = "phone-thumbs-restore"
	}
	ctx := w.begin(job, 0)
	w.mu.Lock()
	p := w.phoneReview
	w.mu.Unlock()
	if p == nil {
		w.finish(job, "", errors.New("请先核对旧封面"))
		return
	}
	if !restore {
		if time.Now().After(p.Expires) {
			w.finish(job, "", errors.New("预览已过期，请重新核对"))
			return
		}
		if err := r.phoneThumbReady(ctx, p); err != nil {
			w.finish(job, "", err)
			return
		}
	}
	w.mu.Lock()
	w.total = len(p.Dirs)
	w.mu.Unlock()
	for start := 0; start < len(p.Dirs); start += 20 {
		var script strings.Builder
		for i := start; i < min(start+20, len(p.Dirs)); i++ {
			d := p.Dirs[i]
			if restore != d.Moved {
				w.progress(0)
				continue
			}
			trash := p.Summary.Trash
			if d.Trash != "" {
				trash = d.Trash
			}
			from, to := path.Join(r.phoneDir, d.Rel), path.Join(trash, d.Rel)
			if restore {
				from, to = to, from
			} else {
				fmt.Fprintf(&script, "current=$(cd %s && find . -mindepth 1 -maxdepth 1 -exec stat -c '%%f|%%n|%%s|%%Y' {} + | sort | sha256sum)\n[ \"${current%%%% *}\" = %s ] || exit 19\n", shellQuote(from), shellQuote(d.Signature))
			}
			fmt.Fprintf(&script, "[ -d %s ] && [ ! -L %s ] && [ ! -e %s ] && mkdir -p %s && mv %s %s || exit 20\necho __OMNI_MOVED_%d\n", shellQuote(from), shellQuote(from), shellQuote(to), shellQuote(path.Dir(to)), shellQuote(from), shellQuote(to), i)
		}
		if script.Len() == 0 {
			continue
		}
		out, err := r.adbRead(ctx, p.Input.Device, "shell", script.String())
		for _, line := range strings.Split(string(out), "\n") {
			raw, ok := strings.CutPrefix(line, "__OMNI_MOVED_")
			if !ok {
				continue
			}
			i, e := strconv.Atoi(raw)
			if e != nil || i < start || i >= min(start+20, len(p.Dirs)) {
				continue
			}
			d := p.Dirs[i]
			w.mu.Lock()
			p.Dirs[i].Moved = !restore
			p.Summary.Moved = !restore
			p.Summary.Remaining = 0
			for _, item := range p.Dirs {
				if !item.Moved {
					p.Summary.Remaining++
				}
			}
			w.mu.Unlock()
			verb := "回收"
			if restore {
				verb = "恢复"
			}
			w.note(verb + " " + d.Rel)
			w.progress(0)
		}
		if err != nil {
			if ctx.Err() != nil {
				w.finish(job, "已取消；已回收的可恢复", nil)
			} else {
				w.finish(job, "已停止；目录变化或连接中断，已回收的可恢复", err)
			}
			return
		}
	}
	note := "旧封面已移入手机回收目录：" + p.Summary.Trash + "；尚未释放存储空间，可恢复"
	if restore {
		note = "旧封面已恢复原位"
	}
	w.finish(job, note, nil)
}

// readPhoneThumbHistory reconstructs the recoverable batches from the phone,
// so restoration does not depend on the host process or its in-memory sessions.
func (r *Router) readPhoneThumbHistory(device string) {
	w := r.svJobs
	ctx := w.begin("phone-thumbs-history", 0)
	root := path.Join(path.Dir(path.Dir(r.phoneDir)), ".omni_data", "trash", "shortvideo-thumbnails")
	w.note("读取手机旧封面回收记录：" + device)
	out, err := r.adbRead(ctx, device, "shell", "[ -d "+shellQuote(root)+" ] && find "+shellQuote(root)+" -mindepth 1 -maxdepth 1 -type d -print | sort")
	if err != nil {
		w.finish("phone-thumbs-history", "", err)
		return
	}
	var batches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := path.Base(line)
		if path.Dir(line) == root && len(name) == 15 && name[8] == '-' {
			if _, e := time.Parse("20060102-150405", name); e == nil {
				batches = append(batches, line)
			}
		}
	}
	if len(batches) == 0 {
		w.finish("phone-thumbs-history", "手机上没有旧封面回收记录", nil)
		return
	}
	p := &phoneThumbReview{Input: phoneThumbInput{Device: device}, Summary: phoneThumbSummary{Device: device, Trash: root, Moved: true}}
	for _, batch := range batches {
		out, err = r.adbRead(ctx, device, "shell", "find "+shellQuote(batch)+" -mindepth 3 -maxdepth 3 -type d -name .thumbs -print")
		if err != nil {
			w.finish("phone-thumbs-history", "", err)
			return
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			rel := strings.TrimPrefix(line, batch+"/")
			if rel == line || path.Clean(rel) != rel || strings.Count(rel, "/") != 2 || path.Base(rel) != ".thumbs" {
				continue
			}
			p.Dirs = append(p.Dirs, phoneThumbDir{Rel: rel, Trash: batch, Moved: true})
		}
	}
	if len(p.Dirs) == 0 {
		w.finish("phone-thumbs-history", "最近一批旧封面已经恢复，回收目录为空", nil)
		return
	}
	// Only read sizes, not the image payloads. Android stat supports this on all
	// devices supported by the existing USB transfer implementation.
	out, err = r.adbRead(ctx, device, "shell", "find "+shellQuote(root)+" -mindepth 5 -maxdepth 5 -type f -exec stat -c '%s' {} +")
	if err != nil {
		w.finish("phone-thumbs-history", "", err)
		return
	}
	for _, line := range strings.Fields(string(out)) {
		if n, e := strconv.ParseInt(line, 10, 64); e == nil && n >= 0 {
			p.Summary.Files++
			p.Summary.Bytes += n
		}
	}
	p.Summary.Directories = len(p.Dirs)
	id := sha256.Sum256([]byte(root + time.Now().String()))
	p.Summary.Token = hex.EncodeToString(id[:16])
	w.mu.Lock()
	old := w.phoneReview
	w.phoneReview = p
	w.mu.Unlock()
	r.closePhoneForward(old)
	w.finish("phone-thumbs-history", fmt.Sprintf("找到回收记录：%d 个目录，%d 张（%.2f GB），可恢复", len(p.Dirs), p.Summary.Files, float64(p.Summary.Bytes)/1e9), nil)
}
