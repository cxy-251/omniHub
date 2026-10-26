package server

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	dlassets "omnihub/features/downloads"
	"omnihub/internal/platform"
	"omnihub/internal/tasks"
)

// 18comic（禁漫）漫画下载的联网部分：搜索、榜单、本子详情和封面（只给桌面端下载页用，路径在 /api/downloads/ 下，
// 只有管理员、不经隧道才能用，见 access.go）。查询交给 features/downloads/scripts/manga.py（jmcomic），
// 每次查询起一个 Python 进程；封面缓存在 cacheDir/jm-covers/<id>.jpg。
// 结果里标出哪些本子已经下载过（各库漫画目录里 CBZ 的 metadata.json 记着本子 ID）。

type jmState struct {
	mu       sync.Mutex
	fetching map[string]bool // 正在下载封面的本子 ID
	ids      map[string]jmCBZ
	active   map[string]string // 正在下载的本子 ID → 任务 ID
}

// jmTrack 记下一个下载任务里的本子，任务结束时去掉。
func (r *Router) jmTrack(job string, ids []string, done <-chan struct{}) {
	r.jm.mu.Lock()
	if r.jm.active == nil {
		r.jm.active = map[string]string{}
	}
	nums := make([]string, len(ids))
	for i, id := range ids {
		nums[i] = jmAlbumNumber(id)
		r.jm.active[nums[i]] = job
	}
	r.jm.mu.Unlock()
	go func() {
		<-done
		r.jm.mu.Lock()
		for _, id := range nums {
			if r.jm.active[id] == job {
				delete(r.jm.active, id)
			}
		}
		r.jm.mu.Unlock()
	}()
}

// jmNotActive 去掉已经在下载任务里的本子（任务里 ID 可能写成网址，按其中的数字比）。
func (r *Router) jmNotActive(ids []string) []string {
	r.jm.mu.Lock()
	defer r.jm.mu.Unlock()
	var out []string
	for _, id := range ids {
		if _, busy := r.jm.active[jmAlbumNumber(id)]; !busy {
			out = append(out, id)
		}
	}
	return out
}

func (r *Router) jmActive(id string) bool {
	r.jm.mu.Lock()
	defer r.jm.mu.Unlock()
	_, ok := r.jm.active[id]
	return ok
}

// jmAlbumNumber 从本子编号或网址里取出数字编号（认不出时原样返回）。
func jmAlbumNumber(s string) string {
	s = strings.TrimSpace(s)
	for _, key := range []string{"/album/", "/photo/"} {
		if i := strings.Index(s, key); i >= 0 {
			s = s[i+len(key):]
			if j := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }); j >= 0 {
				s = s[:j]
			}
			return s
		}
	}
	return strings.TrimPrefix(strings.TrimPrefix(s, "JM"), "jm")
}

// jmCBZ 记着一个 CBZ 的本子 ID，按文件大小和修改时间判断要不要重读。
type jmCBZ struct {
	size  int64
	mtime int64
	id    string
}

type jmAlbum struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	Tags       []string `json:"tags"`
	Chapters   int      `json:"chapters,omitempty"`
	Error      string   `json:"error,omitempty"`
	Downloaded string   `json:"downloaded,omitempty"` // 已下载时为所在资源库的名字
	Active     bool     `json:"active,omitempty"`     // 正在下载任务里
}

func (r *Router) handleJM(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(1, min(page, 500))
	switch strings.TrimPrefix(req.URL.Path, "/api/downloads/manga/") {
	case "search":
		query := strings.TrimSpace(q.Get("q"))
		if query == "" || len(query) > 500 {
			http.Error(w, "请输入关键词、本子编号或网址", http.StatusBadRequest)
			return
		}
		order := q.Get("order")
		if !map[string]bool{"mv": true, "mr": true, "mp": true, "tf": true}[order] {
			order = "mv"
		}
		r.jmList(w, req, "search", "--query", query, "--page", strconv.Itoa(page), "--order", order)
	case "ranking":
		kind := q.Get("type")
		if kind != "day" && kind != "month" {
			kind = "week"
		}
		r.jmList(w, req, "ranking", "--type", kind, "--page", strconv.Itoa(page))
	case "detail":
		id := q.Get("id")
		if _, err := strconv.ParseUint(id, 10, 64); err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		out, err := r.jmQuery(req.Context(), "detail", id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	case "cover":
		r.jmCover(w, req, q.Get("id"))
	case "unfinished":
		writeJSON(w, http.StatusOK, r.jmUnfinished(req.Context()))
	case "downloaded":
		// 已下载过的本子 ID（下载列表加书时过滤用）
		ids := r.jmDownloaded(req.Context())
		out := make([]string, 0, len(ids))
		for id := range ids {
			out = append(out, id)
		}
		writeJSON(w, http.StatusOK, out)
	default:
		http.NotFound(w, req)
	}
}

// jmList 运行搜索或榜单，标出已下载的本子，并在后台把封面先下好。
func (r *Router) jmList(w http.ResponseWriter, req *http.Request, args ...string) {
	raw, err := r.jmQuery(req.Context(), args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	var out struct {
		Results   []jmAlbum `json:"results"`
		Total     int       `json:"total"`
		Page      int       `json:"page"`
		PageCount int       `json:"page_count"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		http.Error(w, "查询结果无法解析", http.StatusBadGateway)
		return
	}
	have := r.jmDownloaded(req.Context())
	ids := make([]string, 0, len(out.Results))
	for i := range out.Results {
		out.Results[i].Downloaded = have[out.Results[i].ID]
		out.Results[i].Active = r.jmActive(out.Results[i].ID)
		if out.Results[i].Tags == nil {
			out.Results[i].Tags = []string{}
		}
		ids = append(ids, out.Results[i].ID)
	}
	if out.Results == nil {
		out.Results = []jmAlbum{}
	}
	r.jmFetchCovers(ids)
	writeJSON(w, http.StatusOK, out)
}

// jmQuery 运行 manga.py 的查询命令，返回它输出的 JSON；脚本报的错（如网络不通）作为 error 返回。
// 第一次运行要由 uv 装依赖，最多等 5 分钟。
func (r *Router) jmQuery(ctx context.Context, args ...string) (json.RawMessage, error) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		return nil, errors.New("需要 uv 来运行漫画脚本")
	}
	dir, err := dlassets.Prepare(filepath.Join(r.cacheDir, "downloads"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := platform.BackgroundCommand(ctx, uv, append([]string{"run", "--frozen", "--project", dir, "python", "scripts/manga.py"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "UV_PROJECT_ENVIRONMENT="+filepath.Join(dir, "venv"), "PYTHONUNBUFFERED=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var result json.RawMessage
	sc := bufio.NewScanner(io.LimitReader(stdout, 8<<20))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "OMNIHUB_JSON "); ok {
			result = json.RawMessage(rest)
		}
	}
	_ = cmd.Wait()
	if result == nil {
		if ctx.Err() != nil {
			return nil, errors.New("查询超时")
		}
		return nil, errors.New("查询没有结果（脚本出错）")
	}
	var failed struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(result, &failed) == nil && failed.Error != "" {
		return nil, errors.New("18comic 查询失败：" + failed.Error)
	}
	return result, nil
}

func (r *Router) jmCoverPath(id string) string {
	return filepath.Join(r.cacheDir, "jm-covers", id+".jpg")
}

// jmFetchCovers 在后台下载还没缓存的封面（一个进程下一批）。
func (r *Router) jmFetchCovers(ids []string) {
	r.jm.mu.Lock()
	if r.jm.fetching == nil {
		r.jm.fetching = map[string]bool{}
	}
	var todo []string
	for _, id := range ids {
		if _, err := os.Stat(r.jmCoverPath(id)); err == nil || r.jm.fetching[id] {
			continue
		}
		r.jm.fetching[id] = true
		todo = append(todo, id)
	}
	r.jm.mu.Unlock()
	if len(todo) == 0 {
		return
	}
	go func() {
		defer func() {
			r.jm.mu.Lock()
			for _, id := range todo {
				delete(r.jm.fetching, id)
			}
			r.jm.mu.Unlock()
		}()
		_, _ = r.jmQuery(context.Background(), append([]string{"covers", "--dir", filepath.Join(r.cacheDir, "jm-covers")}, todo...)...)
	}()
}

// jmCover 发封面；还在下载的等它最多 30 秒。
func (r *Router) jmCover(w http.ResponseWriter, req *http.Request, id string) {
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	path := r.jmCoverPath(id)
	r.jmFetchCovers([]string{id})
	for deadline := time.Now().Add(30 * time.Second); ; {
		if _, err := os.Stat(path); err == nil {
			w.Header().Set("Cache-Control", "private, max-age=86400")
			http.ServeFile(w, req, path)
			return
		}
		r.jm.mu.Lock()
		pending := r.jm.fetching[id]
		r.jm.mu.Unlock()
		if !pending || time.Now().After(deadline) {
			http.NotFound(w, req)
			return
		}
		select {
		case <-req.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// jmDownloaded 返回各在线资源库里已经下载过的本子：ID → 资源库名。
// 读 CBZ 里的 metadata.json；按文件大小和修改时间缓存，第二次起只读新增或变过的包。
func (r *Router) jmDownloaded(ctx context.Context) map[string]string {
	out := map[string]string{}
	seen := map[string]bool{}
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline {
			continue
		}
		dir, err := r.moduleDirectory(lib.RootPath, "media_library/manga", "manga.db")
		if err != nil {
			continue
		}
		files, err := r.mangaCatalog.Files(ctx, dir)
		if err != nil {
			continue
		}
		for _, rel := range files {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			seen[path] = true
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			r.jm.mu.Lock()
			if r.jm.ids == nil {
				r.jm.ids = map[string]jmCBZ{}
			}
			cached, ok := r.jm.ids[path]
			r.jm.mu.Unlock()
			if !ok || cached.size != info.Size() || cached.mtime != info.ModTime().UnixNano() {
				cached = jmCBZ{size: info.Size(), mtime: info.ModTime().UnixNano(), id: cbzAlbumID(path)}
				r.jm.mu.Lock()
				r.jm.ids[path] = cached
				r.jm.mu.Unlock()
			}
			if cached.id != "" {
				out[cached.id] = lib.Name
			}
		}
	}
	r.jm.mu.Lock()
	for path := range r.jm.ids {
		if !seen[path] {
			delete(r.jm.ids, path)
		}
	}
	r.jm.mu.Unlock()
	return out
}

// cbzAlbumID 读 CBZ 里 metadata.json 的 id（18comic 下载的包都有）；没有时为空。
func cbzAlbumID(path string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "metadata.json" || f.UncompressedSize64 > 1<<20 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		var meta struct {
			ID json.RawMessage `json:"id"`
		}
		err = json.NewDecoder(io.LimitReader(rc, 1<<20)).Decode(&meta)
		rc.Close()
		if err != nil {
			return ""
		}
		id := strings.Trim(string(meta.ID), `"`)
		if _, err := strconv.ParseUint(id, 10, 64); err == nil {
			return id
		}
		return ""
	}
	return ""
}

// 没下完的 18comic 任务记在 cacheDir/jm-unfinished.json（服务端重启后任务列表就没了，下载列表开始下载时也清空了）。
// 任务成功结束时去掉；失败、被停止或服务端重启打断的留着，下载页提示「上次有 N 本没下完」，一键加回下载列表。

type jmUnfinishedJob struct {
	IDs     []string          `json:"ids"`
	Titles  map[string]string `json:"titles"`
	Library string            `json:"library"`
}

func (r *Router) jmUnfinishedPath() string { return filepath.Join(r.cacheDir, "jm-unfinished.json") }

func (r *Router) jmLoadUnfinished() map[string]jmUnfinishedJob {
	out := map[string]jmUnfinishedJob{}
	if data, err := os.ReadFile(r.jmUnfinishedPath()); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func (r *Router) jmSaveUnfinished(jobs map[string]jmUnfinishedJob) {
	data, _ := json.MarshalIndent(jobs, "", "  ")
	_ = os.MkdirAll(filepath.Dir(r.jmUnfinishedPath()), 0o755)
	_ = os.WriteFile(r.jmUnfinishedPath(), data, 0o644)
}

// jmRemember 记下任务要下的本子；任务成功结束（退出码 0）时去掉。
func (r *Router) jmRemember(job string, plan dlPlan, t *tasks.Task) {
	r.jm.mu.Lock()
	jobs := r.jmLoadUnfinished()
	ids := make([]string, len(plan.albums))
	for i, id := range plan.albums {
		ids[i] = jmAlbumNumber(id)
	}
	jobs[job] = jmUnfinishedJob{IDs: ids, Titles: plan.albumTitles, Library: plan.lib.ID}
	r.jmSaveUnfinished(jobs)
	r.jm.mu.Unlock()
	go func() {
		<-t.Done()
		if _, _, _, code := t.State(); code != 0 {
			return
		}
		r.jm.mu.Lock()
		jobs := r.jmLoadUnfinished()
		delete(jobs, job)
		r.jmSaveUnfinished(jobs)
		r.jm.mu.Unlock()
	}()
}

type jmPending struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Library string `json:"library"`
}

// jmUnfinished 返回以前的任务里还没下好、现在也没在下的本子；全部下好的记录顺手删掉。
func (r *Router) jmUnfinished(ctx context.Context) []jmPending {
	have := r.jmDownloaded(ctx)
	r.jm.mu.Lock()
	defer r.jm.mu.Unlock()
	jobs := r.jmLoadUnfinished()
	out := []jmPending{}
	seen := map[string]bool{}
	changed := false
	for job, j := range jobs {
		left := 0
		for _, id := range j.IDs {
			if _, done := have[id]; done {
				continue
			}
			left++
			if _, busy := r.jm.active[id]; busy || seen[id] {
				continue
			}
			seen[id] = true
			title := j.Titles[id]
			if title == "" {
				title = "JM" + id
			}
			out = append(out, jmPending{ID: id, Title: title, Library: j.Library})
		}
		if left == 0 {
			delete(jobs, job)
			changed = true
		}
	}
	if changed {
		r.jmSaveUnfinished(jobs)
	}
	return out
}
