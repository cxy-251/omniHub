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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	dlassets "omnihub/features/downloads"
	"omnihub/internal/config"
	"omnihub/internal/library"
	"omnihub/internal/scanner"
	"omnihub/internal/staging"
	"omnihub/internal/tasks"
)

// 下载任务（规格 6.3）：每种任务在这里声明需要的输入（带默认值），客户端按声明生成表单，
// 用户填必填项后点「开始」。脚本以 OMNIHUB_PROGRESS / OMNIHUB_RESULT 行报告进度与结果，
// 服务端解析后给出进度条、保存位置与「去收听」之类的后续动作。
// 脚本在 features/downloads（uv 锁定依赖），以低优先级、独立进程组运行，同一目标目录同时只跑一个。

type dlOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type dlField struct {
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	Type        string     `json:"type"` // lines 多行 | text | toggle | choice
	Placeholder string     `json:"placeholder,omitempty"`
	Help        string     `json:"help,omitempty"`
	Required    bool       `json:"required,omitempty"`
	Advanced    bool       `json:"advanced,omitempty"` // 收在「更多选项」里
	Default     any        `json:"default,omitempty"`
	Options     []dlOption `json:"options,omitempty"`
	// ShowIf：只在其他字段取这些值时显示（如「专辑名」只在「要什么 = 只要音频」时）；值以 ! 开头表示「不等于」
	ShowIf map[string]string `json:"show_if,omitempty"`
}

type dlKind struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Fields      []dlField `json:"fields"`
	Missing     []string  `json:"missing,omitempty"` // 缺少的本机工具；非空时不能运行
	Scheme      *dlScheme `json:"scheme,omitempty"`  // 下载方案（音乐、YouTube、X……）的界面说明
	needs       []string
	build       func(r *Router, p dlParams) (dlPlan, error)
}

type dlParams map[string]any

func (p dlParams) text(name string) string {
	switch v := p[name].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}
func (p dlParams) flag(name string) bool { v, _ := p[name].(bool); return v }
func (p dlParams) lines(name string) []string {
	var out []string
	for _, l := range strings.Split(p.text(name), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

type dlPlan struct {
	title    string
	args     []string // features/downloads 下的脚本及参数
	resource string   // 写入的目标目录（互斥锁）
	lib      *library.Library
	after    func(ctx context.Context) error // 脚本成功后在服务端执行（如更新漫画索引）
}

type dlState struct {
	mu        sync.Mutex
	settings  config.DownloadSettings
	configDir string
	save      func(config.DownloadSettings) error
	kinds     map[string]string // 任务 ID → 种类
}

// ConfigureDownloads 设置下载任务的网络设置、配置目录与保存回调。
func (r *Router) ConfigureDownloads(settings config.DownloadSettings, configDir string, save func(config.DownloadSettings) error) {
	r.downloads.mu.Lock()
	defer r.downloads.mu.Unlock()
	r.downloads.settings, r.downloads.configDir, r.downloads.save = settings, configDir, save
	if r.downloads.kinds == nil {
		r.downloads.kinds = map[string]string{}
	}
}

// countFiles 统计目录下（最多 3 层）的文件数，最多数到 limit。
func countFiles(dir string, depth, limit int) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if n >= limit {
			break
		}
		if e.IsDir() {
			if depth > 0 {
				n += countFiles(filepath.Join(dir, e.Name()), depth-1, limit-n)
			}
		} else {
			n++
		}
	}
	return n
}

// writableLibraryChoice 列出在线可写库供选择；默认选该分类下文件最多的库（通常就是平时存这类内容的库）。
func (r *Router) writableLibraryChoice(category string) ([]dlOption, string) {
	var opts []dlOption
	def, most := "", -1
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline || lib.ReadOnly {
			continue
		}
		opts = append(opts, dlOption{Value: lib.ID, Label: lib.Name})
		if n := countFiles(filepath.Join(lib.RootPath, category), 3, 5000); n > most {
			def, most = lib.ID, n
		}
	}
	if def == "" && len(opts) > 0 {
		def = opts[0].Value
	}
	return opts, def
}

// stagingLibraryChoice 列出可写的在线库作为暂存位置；默认选和 Chrome 下载目录同一块盘的（多为内置存储）。
func (r *Router) stagingLibraryChoice() ([]dlOption, string) {
	home, _ := os.UserHomeDir()
	var opts []dlOption
	def := ""
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline || lib.ReadOnly {
			continue
		}
		opts = append(opts, dlOption{lib.ID, lib.Name})
		if def == "" && staging.SameDisk(lib.RootPath, filepath.Join(home, "Downloads")) {
			def = lib.ID
		}
	}
	if def == "" && len(opts) > 0 {
		def = opts[0].Value
	}
	return opts, def
}

func (r *Router) writableLibrary(id string) (*library.Library, error) {
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.ID == id && lib.IsOnline && !lib.ReadOnly {
			return lib, nil
		}
	}
	return nil, errors.New("请选择一个在线、可写的资源库")
}

func (r *Router) downloadKinds() []*dlKind {
	mangaLibs, mangaDef := r.writableLibraryChoice("media_library/manga")
	kinds := append(r.schemeKinds(), []*dlKind{
		{
			ID: "manga", Title: "漫画", Description: "按本子网址或 ID 整本下载，封成 CBZ 放进漫画库；已有的会自动检查更新。",
			needs: []string{"uv"},
			Fields: []dlField{
				{Name: "ids", Label: "本子", Type: "lines", Required: true, Placeholder: "粘贴本子网址或 ID，一行一个", Help: "例如 https://18comic.vip/album/123456 或 123456"},
				{Name: "library", Label: "资源库", Type: "choice", Options: mangaLibs, Default: mangaDef, Advanced: true},
			},
			build: func(r *Router, p dlParams) (dlPlan, error) {
				ids := p.lines("ids")
				if len(ids) == 0 || len(ids) > 100 {
					return dlPlan{}, errors.New("请粘贴 1–100 个本子")
				}
				lib, err := r.writableLibrary(p.text("library"))
				if err != nil {
					return dlPlan{}, err
				}
				root := filepath.Join(lib.RootPath, "media_library", "manga")
				if err := os.MkdirAll(root, 0o755); err != nil {
					return dlPlan{}, err
				}
				args := append([]string{"scripts/manga.py"}, ids...)
				args = append(args, "--dest", root, "--work", filepath.Join(root, ".omnihub-download"))
				return dlPlan{title: fmt.Sprintf("漫画 · %d 本", len(ids)), args: args, resource: root, lib: lib,
					after: func(ctx context.Context) error {
						_, err := scanner.ScanMangaContext(ctx, root, root)
						return err
					}}, nil
			},
		},
	}...)
	for _, k := range kinds {
		for _, tool := range k.needs {
			if _, err := exec.LookPath(tool); err != nil {
				k.Missing = append(k.Missing, tool)
			}
		}
	}
	return kinds
}

// dlPreset 是保存的一键任务。
type dlPreset struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Label  string   `json:"label"`
	Params dlParams `json:"params"`
}

func (r *Router) presetsPath() string {
	return filepath.Join(r.downloads.configDir, "download_presets.json")
}

func (r *Router) loadPresets() ([]dlPreset, error) {
	data, err := os.ReadFile(r.presetsPath())
	if errors.Is(err, os.ErrNotExist) {
		return []dlPreset{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []dlPreset
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Router) savePresets(list []dlPreset) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.presetsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.presetsPath())
}

// dlJob 是任务卡片：状态、进度、结果与最近几行日志。
type dlJob struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title"`
	Running   bool           `json:"running"`
	StartedAt time.Time      `json:"started_at"`
	EndedAt   *time.Time     `json:"ended_at,omitempty"`
	ExitCode  int            `json:"exit_code"`
	Done      int            `json:"done"`
	Total     int            `json:"total"`
	Step      string         `json:"step"`
	Result    map[string]any `json:"result,omitempty"`
	Open      map[string]any `json:"open,omitempty"` // 后续动作：{type: "audio", album: 逻辑路径}
	Tail      []string       `json:"tail"`
}

const (
	progressPrefix = "OMNIHUB_PROGRESS "
	resultPrefix   = "OMNIHUB_RESULT "
)

func (r *Router) jobCard(t *tasks.Task) dlJob {
	running, started, ended, code := t.State()
	r.downloads.mu.Lock()
	kind := r.downloads.kinds[t.ID]
	r.downloads.mu.Unlock()
	job := dlJob{ID: t.ID, Kind: kind, Title: t.Name, Running: running, StartedAt: started, EndedAt: ended, ExitCode: code, Tail: []string{}}
	for _, e := range t.Logs() {
		switch {
		case strings.HasPrefix(e.Message, progressPrefix):
			parts := strings.SplitN(strings.TrimPrefix(e.Message, progressPrefix), " ", 3)
			if len(parts) >= 2 {
				job.Done, _ = strconv.Atoi(parts[0])
				job.Total, _ = strconv.Atoi(parts[1])
			}
			if len(parts) == 3 {
				job.Step = parts[2]
			}
		case strings.HasPrefix(e.Message, resultPrefix):
			_ = json.Unmarshal([]byte(strings.TrimPrefix(e.Message, resultPrefix)), &job.Result)
		case strings.TrimSpace(e.Message) != "":
			job.Tail = append(job.Tail, e.Message)
		}
	}
	if len(job.Tail) > 3 {
		job.Tail = job.Tail[len(job.Tail)-3:]
	}
	if path, _ := job.Result["path"].(string); path != "" && kind == "audiobook" { // 旧版有声书任务（已并进 YouTube / B 站的「只要音频」）
		if logical := r.logicalPath(path); logical != "" {
			job.Open = map[string]any{"type": "audio", "album": logical}
		}
	}
	return job
}

// logicalPath 把本机路径换成「库ID:库内相对路径」，不在任何库里时返回空。
func (r *Router) logicalPath(path string) string {
	for _, lib := range r.libMgr.ListLibraries() {
		if rel, err := filepath.Rel(lib.RootPath, path); err == nil && filepath.IsLocal(rel) {
			return lib.ID + ":" + filepath.ToSlash(rel)
		}
	}
	return ""
}

// handleDownloads 是下载任务接口（只限本机受信任环境的管理员，access.go 统一检查）。
func (r *Router) handleDownloads(w http.ResponseWriter, req *http.Request) {
	action := strings.TrimPrefix(req.URL.Path, "/api/downloads/")
	if req.Method == http.MethodGet {
		switch action {
		case "kinds":
			writeJSON(w, http.StatusOK, r.downloadKinds())
		case "presets":
			list, err := r.loadPresets()
			if err != nil {
				http.Error(w, "预设文件损坏："+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, list)
		case "jobs":
			out := []dlJob{}
			for _, t := range r.taskMgr.List() {
				if strings.HasPrefix(t.ID, "dl-") {
					out = append(out, r.jobCard(t))
				}
			}
			writeJSON(w, http.StatusOK, out)
		case "log":
			t := r.taskMgr.GetTask(req.URL.Query().Get("id"))
			if t == nil || !strings.HasPrefix(t.ID, "dl-") {
				http.NotFound(w, req)
				return
			}
			after, _ := strconv.ParseInt(req.URL.Query().Get("after"), 10, 64)
			out := []tasks.LogEntry{}
			for _, e := range t.Logs() {
				if e.ID > after && !strings.HasPrefix(e.Message, progressPrefix) && !strings.HasPrefix(e.Message, resultPrefix) {
					out = append(out, e)
				}
			}
			writeJSON(w, http.StatusOK, out)
		case "plan": // 音乐「读取列表」的结果（任务结束后才有）
			path, ok := r.planPath(req.URL.Query().Get("token"))
			if !ok {
				http.Error(w, "invalid token", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			http.ServeFile(w, req, path)
		case "settings":
			r.downloads.mu.Lock()
			s := r.downloads.settings
			r.downloads.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"ytdlp_proxy": s.YtdlpProxy, "has_cookies": s.YtdlpCookies != ""})
		default:
			http.NotFound(w, req)
		}
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 256<<10))
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	switch action {
	case "analyze":
		r.analyzeDownload(w, body)
		return

	case "run":
		var in struct {
			Kind   string   `json:"kind"`
			Params dlParams `json:"params"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		job, err := r.startDownload(in.Kind, in.Params)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, tasks.ErrResourceBusy) {
				status, err = http.StatusConflict, errors.New("同一个目录已有任务在运行，等它结束再试")
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, http.StatusOK, job)
	case "stop":
		var in struct{ ID string }
		if json.Unmarshal(body, &in) != nil || !strings.HasPrefix(in.ID, "dl-") {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := r.taskMgr.StopTask(in.ID); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
	case "presets":
		var p dlPreset
		if json.Unmarshal(body, &p) != nil || p.Kind == "" || strings.TrimSpace(p.Label) == "" || len(p.Label) > 200 {
			http.Error(w, "预设需要种类和名称", http.StatusBadRequest)
			return
		}
		list, err := r.loadPresets()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(list) >= 200 {
			http.Error(w, "预设太多了", http.StatusBadRequest)
			return
		}
		buf := make([]byte, 6)
		_, _ = rand.Read(buf)
		p.ID, p.Label = hex.EncodeToString(buf), strings.TrimSpace(p.Label)
		if err := r.savePresets(append(list, p)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, p)
	case "presets/delete":
		var in struct{ ID string }
		if json.Unmarshal(body, &in) != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		list, err := r.loadPresets()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		kept := list[:0]
		for _, p := range list {
			if p.ID != in.ID {
				kept = append(kept, p)
			}
		}
		if err := r.savePresets(kept); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "settings":
		r.saveDownloadSettings(w, body)
	default:
		http.NotFound(w, req)
	}
}

// saveDownloadSettings 保存代理；给出 cookies_file 时把该文件复制进配置目录（0600），清空时传 clear_cookies。
func (r *Router) saveDownloadSettings(w http.ResponseWriter, body []byte) {
	var in struct {
		Proxy        string `json:"ytdlp_proxy"`
		CookiesFile  string `json:"cookies_file"`
		ClearCookies bool   `json:"clear_cookies"`
	}
	if json.Unmarshal(body, &in) != nil || len(in.Proxy) > 512 {
		http.Error(w, "invalid settings", http.StatusBadRequest)
		return
	}
	r.downloads.mu.Lock()
	defer r.downloads.mu.Unlock()
	next := r.downloads.settings
	next.YtdlpProxy = strings.TrimSpace(in.Proxy)
	if in.ClearCookies {
		next.YtdlpCookies = ""
	}
	if in.CookiesFile != "" {
		data, err := os.ReadFile(in.CookiesFile)
		if err != nil || len(data) > 4<<20 {
			http.Error(w, "读不了这个 Cookie 文件", http.StatusBadRequest)
			return
		}
		target := filepath.Join(r.downloads.configDir, "ytdlp-cookies.txt")
		if err := os.WriteFile(target, data, 0o600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		next.YtdlpCookies = target
	}
	if r.downloads.save != nil {
		if err := r.downloads.save(next); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
	}
	r.downloads.settings = next
	writeJSON(w, http.StatusOK, map[string]any{"ytdlp_proxy": next.YtdlpProxy, "has_cookies": next.YtdlpCookies != ""})
}

func (r *Router) startDownload(kindID string, params dlParams) (dlJob, error) {
	var kind *dlKind
	for _, k := range r.downloadKinds() {
		if k.ID == kindID {
			kind = k
		}
	}
	if kind == nil {
		return dlJob{}, errors.New("unknown download kind")
	}
	if len(kind.Missing) > 0 {
		return dlJob{}, fmt.Errorf("本机缺少：%s", strings.Join(kind.Missing, "、"))
	}
	// 未填的项用声明里的默认值。
	if params == nil {
		params = dlParams{}
	}
	for _, f := range kind.Fields {
		if _, ok := params[f.Name]; !ok && f.Default != nil {
			params[f.Name] = f.Default
		}
	}
	plan, err := kind.build(r, params)
	if err != nil {
		return dlJob{}, err
	}
	dir, err := dlassets.Prepare(filepath.Join(r.cacheDir, "downloads"))
	if err != nil {
		return dlJob{}, err
	}
	uv, _ := exec.LookPath("uv")
	r.downloads.mu.Lock()
	settings := r.downloads.settings
	r.downloads.mu.Unlock()
	platforms, _ := json.Marshal(cookiePlatforms)
	env := []string{"UV_PROJECT_ENVIRONMENT=" + filepath.Join(dir, "venv"), "PYTHONUNBUFFERED=1", "OMNIHUB_COOKIES_DIR=" + r.cookieDir(), "OMNIHUB_PLATFORMS=" + string(platforms)}
	if settings.YtdlpProxy != "" {
		env = append(env, "OMNIHUB_YTDLP_PROXY="+settings.YtdlpProxy)
	}
	if settings.YtdlpCookies != "" {
		env = append(env, "OMNIHUB_YTDLP_COOKIES="+settings.YtdlpCookies)
	}
	if settings.LastfmKey != "" {
		env = append(env, "OMNIHUB_LASTFM_KEY="+settings.LastfmKey)
	}
	if err := os.MkdirAll(plan.resource, 0o755); err != nil {
		return dlJob{}, err
	}
	id := fmt.Sprintf("dl-%s-%d", kind.ID, time.Now().UnixNano())
	t, err := r.taskMgr.RunCommand(id, tasks.Command{
		Name: plan.title, Executable: uv, Directory: dir, Resource: plan.resource, Write: true,
		Args:        append([]string{"run", "--frozen", "--project", dir, "python"}, plan.args...),
		Environment: env,
	})
	if err != nil {
		return dlJob{}, err
	}
	r.downloads.mu.Lock()
	r.downloads.kinds[id] = kind.ID
	r.downloads.mu.Unlock()
	if plan.after != nil {
		go func() {
			<-t.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := plan.after(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "download %s: post-processing failed: %v\n", id, err)
			}
		}()
	}
	return r.jobCard(t), nil
}

// analyzeDownload 预览通用下载：每个链接会用什么工具、带不带登录、下到哪里，以及暂存批次放在哪个库。
// 只看链接的域名，不联网；界面边输入边调用。
func (r *Router) analyzeDownload(w http.ResponseWriter, body []byte) {
	var in struct {
		URLs    string `json:"urls"`
		Mode    string `json:"mode"`
		Library string `json:"library"`
	}
	if json.Unmarshal(body, &in) != nil || len(in.URLs) > 64<<10 {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	type item struct {
		URL      string `json:"url"`
		Valid    bool   `json:"valid"`
		Platform string `json:"platform"`
		Tool     string `json:"tool"`
		Cookie   string `json:"cookie"` // logged_in | no_login | none | unknown_site
		Target   string `json:"target"`
	}
	dir := r.cookieDir()
	items := []item{}
	for _, line := range strings.Split(in.URLs, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || len(items) >= 200 {
			continue
		}
		it := item{URL: line}
		u, err := url.Parse(line)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			it.Target = "不是网址，会被拒绝"
			items = append(items, it)
			continue
		}
		it.Valid = true
		p := platformOf(u.Hostname())
		dirName := strings.TrimPrefix(u.Hostname(), "www.")
		it.Platform, it.Tool, it.Cookie = dirName, "yt-dlp", "unknown_site"
		if p != nil {
			it.Platform, it.Tool, dirName, it.Cookie = p.Label, p.Tool, p.Dir, "none"
			if data, err := os.ReadFile(filepath.Join(dir, p.ID+".json")); err == nil {
				var info cookieInfo
				if json.Unmarshal(data, &info) == nil {
					it.Cookie = "no_login"
					if info.LoggedIn && (info.Expires == 0 || info.Expires > float64(time.Now().Unix())) {
						it.Cookie = "logged_in"
					}
				}
			}
		}
		switch {
		case it.Tool == "gallery-dl":
			it.Target = "图片 → 暂存区 → 移入时并进相册"
		case in.Mode == "audio":
			it.Target = "音频 → 暂存区 → 音声/音乐/<播放列表或专辑>/"
		default:
			it.Target = "视频 → 暂存区 → 短视频/" + dirName + "/<作者>/"
		}
		items = append(items, it)
	}
	out := map[string]any{"items": items}
	if lib, err := r.writableLibrary(in.Library); err == nil {
		home, _ := os.UserHomeDir()
		out["library"] = map[string]any{"id": lib.ID, "name": lib.Name, "staging": filepath.Join(lib.RootPath, staging.Dir),
			"near_downloads": staging.SameDisk(lib.RootPath, filepath.Join(home, "Downloads"))}
	}
	writeJSON(w, http.StatusOK, out)
}
