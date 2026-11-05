package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sshkeys "golang.org/x/crypto/ssh"
	"omnihub/internal/auth"
	"omnihub/internal/buildinfo"
	"omnihub/internal/phoneops"
	"omnihub/internal/platform"
)

type deviceInput struct {
	AdminName     string   `json:"admin_name"`
	AdminPassword string   `json:"admin_password"`
	Device        string   `json:"device"`
	TargetRoot    string   `json:"target_root"`
	LibraryID     string   `json:"library_id"`
	All           bool     `json:"all"`
	Selection     []string `json:"selection"`
	ReserveGiB    int64    `json:"reserve_gib"`
	RemoveSource  bool     `json:"remove_source"`
	PlanID        string   `json:"plan_id"`
	ProjectDir    string   `json:"project_dir"`
	phoneops.SSHOptions
}
type deviceProbe struct {
	Device    string `json:"device"`
	Identity  string `json:"identity"`
	USB       bool   `json:"usb"`
	ABI       string `json:"abi"`
	Root      string `json:"root"`
	Free      int64  `json:"free"`
	RootError string `json:"root_error"`
	SSHReady  bool   `json:"ssh_ready"`
	SSHError  string `json:"ssh_error"`
	Install   string `json:"install_dir"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
}
type planView struct {
	ID           string `json:"id"`
	Device       string `json:"device"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	Count        int    `json:"count"`
	Done         int    `json:"done"`
	Total        int64  `json:"total"`
	Needed       int64  `json:"needed"`
	Free         int64  `json:"free"`
	Reserve      int64  `json:"reserve"`
	RemoveSource bool   `json:"remove_source"`
}

func viewPlan(p *phoneops.MangaPlan) *planView {
	done := 0
	for _, f := range p.Files {
		if f.Complete && (!p.RemoveSource || f.Recycled) {
			done++
		}
	}
	needed := p.Needed
	if done == len(p.Files) {
		needed = 0
	}
	return &planView{ID: p.ID, Device: p.Device, Source: p.SourceName, Target: p.Target, Count: len(p.Files), Done: done, Total: p.Total, Needed: needed, Free: p.Free, Reserve: p.Reserve, RemoveSource: p.RemoveSource}
}

type deviceStatus struct {
	Running  bool              `json:"running"`
	Kind     string            `json:"kind"`
	Message  string            `json:"message"`
	Error    string            `json:"error"`
	Progress phoneops.Progress `json:"progress"`
	Logs     []string          `json:"logs"`
	Plan     *planView         `json:"plan"`
	Probe    *deviceProbe      `json:"probe"`
}
type deviceOperations struct {
	mu     sync.Mutex
	status deviceStatus
	cancel context.CancelFunc
	done   chan struct{}
	dir    string
}

func newDeviceOperations(dir string) *deviceOperations {
	d := &deviceOperations{dir: dir, status: deviceStatus{Logs: []string{}}}
	if p, err := d.readPlan(); err == nil {
		d.status.Plan = viewPlan(p)
		d.status.Message = "发现上次漫画计划，连接 USB 后可继续；已完成文件会校验后跳过"
	}
	return d
}
func (d *deviceOperations) snapshot() deviceStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.status
	s.Logs = append([]string{}, s.Logs...)

	return s
}
func (d *deviceOperations) note(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.Message = s
	d.status.Logs = append(d.status.Logs, time.Now().Format("15:04:05")+" "+s)
	if len(d.status.Logs) > 120 {
		d.status.Logs = d.status.Logs[len(d.status.Logs)-120:]
	}
}
func (d *deviceOperations) close() {
	d.mu.Lock()
	cancel, done := d.cancel, d.done
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
func (d *deviceOperations) readPlan() (*phoneops.MangaPlan, error) {
	f, err := os.Open(filepath.Join(d.dir, "manga-plan.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var p phoneops.MangaPlan
	err = json.NewDecoder(http.MaxBytesReader(nil, f, 32<<20)).Decode(&p)
	return &p, err
}
func (d *deviceOperations) savePlan(p *phoneops.MangaPlan) error {
	if err := os.MkdirAll(d.dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(d.dir, ".plan-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(d.dir, "manga-plan.json"))
}
func (r *Router) startDeviceJob(kind string, fn func(context.Context) error) bool {
	d := r.deviceOps
	d.mu.Lock()
	if d.status.Running {
		d.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.done = make(chan struct{})
	done := d.done
	d.status.Running = true
	d.status.Kind = kind
	d.status.Error = ""
	d.status.Progress = phoneops.Progress{}
	d.mu.Unlock()
	go func() {
		defer close(done)
		defer cancel()
		r.runtimeMu.RLock()
		defer r.runtimeMu.RUnlock()
		err := fn(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				if kind == "deploy" {
					d.note("部署任务已取消，本机构建已停止")
				} else if strings.HasPrefix(kind, "manga/") {
					d.note("已暂停；漫画断点保留，可继续传输")
				} else {
					d.note("已停止检查")
				}
			} else {
				d.note("任务停止：" + err.Error())
			}
		} else {
			d.note("操作完成")
		}
		d.mu.Lock()
		d.status.Running = false
		d.cancel = nil
		if err != nil {
			d.status.Error = err.Error()
		}
		d.mu.Unlock()
	}()
	return true
}
func deviceID() string { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func (r *Router) deviceADB(in deviceInput) phoneops.ADB {
	return phoneops.ADB{Path: r.phoneADB, Serial: in.Device}
}
func (r *Router) selectedDevice(ctx context.Context, in deviceInput) (phoneops.ADB, string, error) {
	if in.Device == "" || len(in.Device) > 256 || strings.ContainsAny(in.Device, "\x00\n\r") {
		return phoneops.ADB{}, "", errors.New("请选择设备")
	}
	a := r.deviceADB(in)
	id, err := a.Identity(ctx)
	return a, id, err
}
func (r *Router) standardSource(id string, remove bool) (string, string, error) {
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.ID == id && lib.IsOnline {
			if remove && lib.ReadOnly {
				return "", "", errors.New("只读资源库不能回收来源文件")
			}
			p, err := r.libMgr.ResolveCategoryPath(id+":media_library/manga/standard", "media_library/manga")
			return p, lib.Name, err
		}
	}
	return "", "", errors.New("请选择在线资源库")
}

// All device mutation endpoints require native-local authentication and an administrator.
func (r *Router) handleDeviceOperations(w http.ResponseWriter, req *http.Request) {
	if !r.gameControlAllowed(w, req) {
		return
	}
	if auth.UserFromContext(req.Context()).Role != auth.RoleAdmin {
		http.Error(w, "需要管理员账号", 403)
		return
	}
	action := strings.TrimPrefix(req.URL.Path, "/api/devices/")
	d := r.deviceOps
	if req.Method == http.MethodGet {
		switch action {
		case "status":
			writeJSON(w, 200, d.snapshot())
			return
		case "list":
			ctx, cancel := context.WithTimeout(req.Context(), 15*time.Second)
			defer cancel()
			devices, err := phoneops.Devices(ctx, r.phoneADB)
			key := ""
			if home, e := os.UserHomeDir(); e == nil {
				candidate := filepath.Join(home, ".ssh", "phone_ed25519")
				if st, e := os.Stat(candidate); e == nil && st.Mode().IsRegular() {
					key = candidate
				}
			}
			ssh, _ := exec.LookPath("ssh")
			message := ""
			if err != nil {
				message = err.Error()
			}
			if devices == nil {
				devices = []phoneops.Device{}
			}
			libraries := r.libMgr.ListLibraries()
			defaultLibrary := ""
			for _, lib := range libraries {
				if root, _, e := r.standardSource(lib.ID, false); e == nil {
					if st, e := os.Stat(root); e == nil && st.IsDir() {
						defaultLibrary = lib.ID
						break
					}
				}
			}
			writeJSON(w, 200, map[string]any{"devices": devices, "error": message, "adb": r.phoneADB != "", "ssh": ssh != "", "default_root": path.Dir(path.Dir(r.phoneDir)), "default_key": key, "default_manga_library": defaultLibrary, "libraries": libraries, "host_version": buildinfo.Description(), "default_project": phoneops.DefaultProject(), "build_tools": phoneops.BuildTools()})
			return
		case "setup-command":
			key := req.URL.Query().Get("key_file")
			if !filepath.IsAbs(key) {
				http.Error(w, "请先选择 SSH 私钥（需有对应 .pub 文件）", 400)
				return
			}
			f, err := os.Open(key + ".pub")
			if err != nil {
				http.Error(w, "无法读取对应公钥文件", 400)
				return
			}
			data, err := io.ReadAll(io.LimitReader(f, 16385))
			f.Close()
			if err != nil || len(data) > 16384 {
				http.Error(w, "无法读取对应公钥文件", 400)
				return
			}
			pub, _, _, _, err := sshkeys.ParseAuthorizedKey(data)
			if err != nil {
				http.Error(w, "公钥格式无效", 400)
				return
			}
			value := strings.TrimSpace(string(sshkeys.MarshalAuthorizedKey(pub)))
			script := "pkg install -y openssh curl && mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && (grep -qxF " + phoneops.Quote(value) + " ~/.ssh/authorized_keys || printf '%s\n' " + phoneops.Quote(value) + " >> ~/.ssh/authorized_keys) && chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys && sshd"
			writeJSON(w, 200, map[string]string{"command": script})
			return
		case "catalog":
			root, _, err := r.standardSource(req.URL.Query().Get("library_id"), false)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			files, err := phoneops.Catalogue(root)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			writeJSON(w, 200, files)
			return
		default:
			http.NotFound(w, req)
			return
		}
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if action == "pause" {
		d.mu.Lock()
		if d.cancel != nil {
			d.cancel()
		}
		d.mu.Unlock()
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	var in deviceInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		http.Error(w, "请求格式错误；若客户端尚未更新，请关闭后重新打开", 400)
		return
	}
	var job func(context.Context) error
	switch action {
	case "probe":
		job = func(ctx context.Context) error { return r.probeDevice(ctx, in) }

	case "manga/plan":
		job = func(ctx context.Context) error { return r.planDeviceManga(ctx, in) }
	case "manga/start":
		job = func(ctx context.Context) error { return r.runDeviceManga(ctx, in) }
	case "deploy":
		job = func(ctx context.Context) error { return r.deployDevice(ctx, in) }
	default:
		http.NotFound(w, req)
		return
	}
	if !r.startDeviceJob(action, job) {
		http.Error(w, "已有设备任务进行中，请等待或暂停", 409)
		return
	}
	writeJSON(w, 202, d.snapshot())
}
func (r *Router) probeDevice(ctx context.Context, in deviceInput) error {
	d := r.deviceOps
	d.note("检查 ADB、手机资源库与 Termux SSH")
	a, id, err := r.selectedDevice(ctx, in)
	if err != nil {
		return err
	}
	p := deviceProbe{Device: in.Device, Identity: id, USB: a.RequireUSB(ctx) == nil}
	if out, e := a.Shell(ctx, "getprop ro.product.cpu.abi"); e == nil {
		p.ABI = strings.TrimSpace(string(out))
	}
	root, e := phoneops.LibraryRoot(ctx, a, in.TargetRoot)
	if e != nil {
		p.RootError = e.Error()
	} else {
		p.Root = root
		p.Free, e = phoneops.Free(ctx, a, root)
		if e != nil {
			p.RootError = e.Error()
		}
	}
	ssh, _ := exec.LookPath("ssh")
	s, e := phoneops.ConnectSSH(ctx, a, id, ssh, d.dir, in.SSHOptions)
	if e == nil {
		defer s.Close()
		p.Install, e = phoneops.InstallPath(ctx, s, in.Install)
		if e == nil {
			out, versionErr := s.Shell(ctx, "if test -x "+phoneops.Quote(path.Join(p.Install, "omnihub-server"))+"; then "+phoneops.Quote(path.Join(p.Install, "omnihub-server"))+" --version; else echo NOT_INSTALLED; fi")
			if versionErr != nil {
				e = versionErr
			} else {
				p.Version = strings.TrimSpace(string(out))
				p.Installed = p.Version != "NOT_INSTALLED"
				p.SSHReady = true
				if p.Installed {
					_, runningErr := s.Shell(ctx, phoneops.Quote(path.Join(p.Install, "omnihub.sh"))+" status")
					p.Running = runningErr == nil
				}
			}
		}
	}
	if e != nil {
		p.SSHError = e.Error()
	}
	d.mu.Lock()
	d.status.Probe = &p
	d.mu.Unlock()
	if p.USB {
		d.note("已连接 USB，可迁移漫画")
	} else {
		d.note("当前是无线连接：可更新程序，迁移漫画请插数据线")
	}
	return nil
}
func (r *Router) planDeviceManga(ctx context.Context, in deviceInput) error {
	d := r.deviceOps
	d.note("检查 USB 连线并统计普通漫画")
	a, id, err := r.selectedDevice(ctx, in)
	if err != nil {
		return err
	}
	if err = a.RequireUSB(ctx); err != nil {
		return err
	}
	remote := phoneops.WiredADB{ADB: a}
	source, name, err := r.standardSource(in.LibraryID, in.RemoveSource)
	if err != nil {
		return err
	}
	files, err := phoneops.Catalogue(source)
	if err != nil {
		return err
	}
	if !in.All {
		wanted := map[string]bool{}
		for _, rel := range in.Selection {
			wanted[rel] = true
		}
		selected := []phoneops.File{}
		for _, f := range files {
			if wanted[f.Rel] {
				selected = append(selected, f)
				delete(wanted, f.Rel)
			}
		}
		if len(wanted) > 0 {
			return errors.New("选中的漫画已变化，请刷新列表")
		}
		files = selected
	}
	if len(files) == 0 {
		return errors.New("没有选中的普通漫画（仅迁移 standard 目录的 CBZ）")
	}
	if in.ReserveGiB < 1 || in.ReserveGiB > 1024 {
		return errors.New("手机预留空间需为 1–1024 GiB")
	}
	target, err := phoneops.LibraryRoot(ctx, remote, in.TargetRoot)
	if err != nil {
		return err
	}
	p := &phoneops.MangaPlan{ID: deviceID(), Device: in.Device, Identity: id, LibraryID: in.LibraryID, SourceName: name, Source: source, Target: target, Reserve: in.ReserveGiB << 30, RemoveSource: in.RemoveSource, Files: files}
	// Rechecking the same selection must retain its byte-level staging paths.
	if previous, e := d.readPlan(); e == nil && sameMangaSelection(previous, p) {
		p.ID, p.Files = previous.ID, previous.Files
	}
	d.note(fmt.Sprintf("检查手机目标与空间：%d 本", len(files)))
	if err = phoneops.Preview(ctx, remote, p); err != nil {
		return err
	}
	if err = d.savePlan(p); err != nil {
		return err
	}
	d.mu.Lock()
	d.status.Plan = viewPlan(p)
	d.mu.Unlock()
	d.note(fmt.Sprintf("计划就绪：%d 本，待传 %.2f GiB；手机剩余 %.2f GiB", len(files), float64(p.Needed)/(1<<30), float64(p.Free)/(1<<30)))
	return nil
}
func (r *Router) runDeviceManga(ctx context.Context, in deviceInput) error {
	d := r.deviceOps
	p, err := d.readPlan()
	if err != nil {
		return errors.New("请先检查迁移计划")
	}
	if p.ID != in.PlanID {
		return errors.New("计划已变化，请重新查看")
	}
	a, id, err := r.selectedDevice(ctx, in)
	if err != nil {
		return err
	}
	if id != p.Identity {
		return errors.New("当前设备不是计划中的手机，拒绝传输")
	}
	if err = a.RequireUSB(ctx); err != nil {
		return err
	}
	source, _, err := r.standardSource(p.LibraryID, p.RemoveSource)
	if err != nil {
		return err
	}
	if source != p.Source {
		return errors.New("来源资源库位置已变化，请重新检查")
	}
	target, err := phoneops.LibraryRoot(ctx, a, p.Target)
	if err != nil {
		return err
	}
	if target != p.Target {
		return errors.New("手机资源库位置变化，请重新检查")
	}
	d.note("USB 连线确认，开始或继续漫画迁移；断开后保留已写入字节")
	err = phoneops.RunManga(ctx, phoneops.WiredADB{ADB: a}, p, func(plan *phoneops.MangaPlan) error {
		if e := d.savePlan(plan); e != nil {
			return e
		}
		d.mu.Lock()
		d.status.Plan = viewPlan(plan)
		d.mu.Unlock()
		return nil
	}, func(progress phoneops.Progress) { d.mu.Lock(); d.status.Progress = progress; d.mu.Unlock() }, platform.Trash)
	if err == nil {
		d.note("漫画已传入手机资源库；手机漫画页面刷新即可查看")
	}
	return err
}
func (r *Router) deployDevice(ctx context.Context, in deviceInput) error {
	d := r.deviceOps
	d.note("检查设备和 Termux 部署环境（支持无线 ADB）")
	a, id, err := r.selectedDevice(ctx, in)
	if err != nil {
		return err
	}
	out, err := a.Shell(ctx, "getprop ro.product.cpu.abi")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "arm64-v8a" {
		return errors.New("目前支持在 arm64 手机的 Termux 中部署服务端")
	}
	ssh, _ := exec.LookPath("ssh")
	s, err := phoneops.ConnectSSH(ctx, a, id, ssh, d.dir, in.SSHOptions)
	if err != nil {
		return err
	}
	defer s.Close()
	install, err := phoneops.InstallPath(ctx, s, in.Install)
	if err != nil {
		return err
	}
	out, err = s.Shell(ctx, "if test -f "+phoneops.Quote(path.Join(install, "omnihub-server"))+"; then echo installed; else echo fresh; fi")
	if err != nil {
		return err
	}
	fresh := strings.TrimSpace(string(out)) == "fresh"
	if fresh && (strings.TrimSpace(in.AdminName) == "" || len(strings.TrimSpace(in.AdminName)) > 64 || len(in.AdminPassword) < 8 || len(in.AdminPassword) > 72 || strings.ContainsAny(in.AdminPassword, "\r\n")) {
		return errors.New("首次部署请填写管理员名称（最多 64 字节）和 8–72 字节密码，密码不能含换行")
	}
	// Everything before Deploy's final switch is non-destructive to the existing installation.
	if err = os.MkdirAll(d.dir, 0700); err != nil {
		return err
	}
	work, err := os.MkdirTemp(d.dir, "package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	project := in.ProjectDir
	if project == "" {
		project = phoneops.DefaultProject()
	}
	extracted := filepath.Join(work, "server")
	if err = phoneops.BuildServer(ctx, project, extracted, d.note); err != nil {
		return err
	}

	free, err := phoneops.Free(ctx, s, path.Dir(install))
	if err != nil {
		return err
	}
	var needed int64
	for _, name := range []string{"omnihub-server", "omnihub.sh"} {
		st, e := os.Stat(filepath.Join(extracted, name))
		if e != nil {
			return e
		}
		needed += st.Size() * 3
	}
	if free < needed+(256<<20) {
		return errors.New("Termux 存储空间不足，更新需容纳新程序、旧版备份并预留 256 MiB")
	}
	if err = phoneops.Deploy(ctx, s, install, extracted, in.ServerPort, d.note); err != nil {
		return err
	}
	if fresh {
		d.note("创建手机首个管理员")
		if err = s.Append(ctx, phoneops.Quote(path.Join(install, "omnihub-server"))+" --setup-admin "+phoneops.Quote(strings.TrimSpace(in.AdminName)), strings.NewReader(in.AdminPassword+"\n")); err != nil {
			return fmt.Errorf("程序已安装，但管理员创建失败，请在手机 Termux 运行 omnihub-server --setup-admin：%w", err)
		}
	}
	if version, e := s.Shell(ctx, phoneops.Quote(path.Join(install, "omnihub-server"))+" --version"); e == nil {
		d.mu.Lock()
		if d.status.Probe != nil && d.status.Probe.Device == in.Device {
			updated := *d.status.Probe
			updated.Version, updated.Installed, updated.Running = strings.TrimSpace(string(version)), true, true
			d.status.Probe = &updated
		}
		d.mu.Unlock()
	} else {
		d.note("服务已更新，暂未读到新版本号；可重新检查设备")
	}
	return nil
}

func sameMangaSelection(a, b *phoneops.MangaPlan) bool {
	if a.Identity != b.Identity || a.LibraryID != b.LibraryID || a.Source != b.Source || a.Target != b.Target || a.RemoveSource != b.RemoveSource || len(a.Files) != len(b.Files) {
		return false
	}
	for i, f := range a.Files {
		g := b.Files[i]
		if f.Rel != g.Rel || f.Size != g.Size || f.Modified != g.Modified {
			return false
		}
	}
	return true
}
