package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"

	"omnihub/internal/library"
	"omnihub/internal/platform"
	"omnihub/internal/shortvideo"
)

// 短视频后台工作：同一时间只执行一件事（扫描 / 图集入库 / 清理原目录 / 生成缩略图），
// 外部进程均以 nice + ionice 低优先级运行。显式任务优先于缩略图生成。
type svWorker struct {
	mu        sync.Mutex
	thumbs    bool
	persist   func(bool) error
	job       string
	done      int
	total     int
	lastJob   string
	lastError string
	lastNote  string
	cancelJob context.CancelFunc

	requests chan string
	wake     chan struct{}
	ctx      context.Context
	stop     context.CancelFunc
	finished chan struct{}
}

// svStatus 是管理页显示的工作状态。
type svStatus struct {
	Thumbnails bool   `json:"thumbnails_enabled"`
	Job        string `json:"job"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	LastJob    string `json:"last_job"`
	LastError  string `json:"last_error"`
	LastNote   string `json:"last_note"`
}

func newSVWorker() *svWorker {
	ctx, stop := context.WithCancel(context.Background())
	return &svWorker{requests: make(chan string, 4), wake: make(chan struct{}, 1), ctx: ctx, stop: stop, finished: make(chan struct{})}
}

// ConfigureShortVideo 设置缩略图开关的初始值及持久化回调（由应用层写入配置文件）。
func (r *Router) ConfigureShortVideo(thumbnails bool, persist func(bool) error) {
	w := r.svJobs
	w.mu.Lock()
	w.thumbs, w.persist = thumbnails, persist
	w.mu.Unlock()
	w.poke()
}

func (w *svWorker) poke() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *svWorker) close() {
	w.stop()
	<-w.finished
}

func (w *svWorker) status() svStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return svStatus{w.thumbs, w.job, w.done, w.total, w.lastJob, w.lastError, w.lastNote}
}

func (w *svWorker) thumbsEnabled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.thumbs
}

func (w *svWorker) begin(job string, total int) context.Context {
	ctx, cancel := context.WithCancel(w.ctx)
	w.mu.Lock()
	w.job, w.done, w.total, w.cancelJob = job, 0, total, cancel
	w.mu.Unlock()
	return ctx
}

func (w *svWorker) progress(total int) {
	w.mu.Lock()
	w.done++
	if total > 0 {
		w.total = total
	}
	w.mu.Unlock()
}

func (w *svWorker) finish(job string, note string, err error) {
	w.mu.Lock()
	if w.cancelJob != nil {
		w.cancelJob()
	}
	w.job, w.cancelJob, w.lastJob, w.lastNote = "", nil, job, note
	w.lastError = ""
	if err != nil {
		w.lastError = err.Error()
		log.Printf("shortvideo %s: %v", job, err)
	}
	w.mu.Unlock()
}

// runShortVideoWorker 是工作循环；路由关闭时退出。启动后先扫描一次所有可写库。
func (r *Router) runShortVideoWorker() {
	w := r.svJobs
	defer close(w.finished)
	w.requests <- "scan"
	for {
		select {
		case <-w.ctx.Done():
			return
		case job := <-w.requests:
			r.runShortVideoJob(job)
			continue
		default:
		}
		if w.thumbsEnabled() && r.thumbnailPass() > 0 {
			continue
		}
		select {
		case <-w.ctx.Done():
			return
		case job := <-w.requests:
			r.runShortVideoJob(job)
		case <-w.wake:
		}
	}
}

// writableStores 列出在线可写库的短视频库（不存在的分类目录跳过）。
func (r *Router) writableStores() (stores []*shortvideo.Store, dirs []string, libs []*library.Library) {
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline || lib.ReadOnly || r.libMgr.IsCategoryDisabled(shortVideoCategory) {
			continue
		}
		store, dir, err := r.svStore(lib)
		if err != nil {
			continue
		}
		stores, dirs, libs = append(stores, store), append(dirs, dir), append(libs, lib)
	}
	return
}

func (r *Router) runShortVideoJob(job string) {
	w := r.svJobs
	stores, dirs, _ := r.writableStores()
	switch job {
	case "scan":
		ctx := w.begin(job, len(stores))
		var total shortvideo.ScanStats
		var errs []error
		for i, s := range stores {
			st, err := s.Scan(ctx, dirs[i])
			errs = append(errs, err)
			total.Added, total.Updated, total.Removed = total.Added+st.Added, total.Updated+st.Updated, total.Removed+st.Removed
			w.progress(0)
		}
		w.finish(job, fmt.Sprintf("新增 %d，更新 %d，移除 %d", total.Added, total.Updated, total.Removed), errors.Join(errs...))
		w.poke()
	case "import", "clean":
		lists := make([][]shortvideo.Post, len(stores))
		count := 0
		for i, s := range stores {
			var err error
			if job == "import" {
				lists[i], err = s.AlbumsToImport(1 << 20)
			} else {
				lists[i], err = s.AlbumsToClean(1 << 20)
			}
			if err != nil {
				w.finish(job, "", err)
				return
			}
			count += len(lists[i])
		}
		ctx := w.begin(job, count)
		ok, failed := 0, 0
		var errs []error
		for i, s := range stores {
			for _, p := range lists[i] {
				if ctx.Err() != nil {
					w.finish(job, fmt.Sprintf("已取消：完成 %d，失败 %d", ok, failed), nil)
					return
				}
				var err error
				if job == "import" {
					err = s.ImportAlbum(dirs[i], p)
				} else {
					err = s.VerifyAndTrash(dirs[i], p, platform.Trash)
				}
				if err != nil {
					failed++
					if len(errs) < 3 {
						errs = append(errs, err)
					}
				} else {
					ok++
				}
				w.progress(0)
			}
		}
		w.finish(job, fmt.Sprintf("完成 %d，失败 %d", ok, failed), errors.Join(errs...))
		w.poke()
	}
}

// thumbnailPass 为各库生成一小批缩略图，返回处理数量；有显式任务等待或开关关闭时提前返回。
func (r *Router) thumbnailPass() int {
	w := r.svJobs
	stores, dirs, _ := r.writableStores()
	processed := 0
	for i, s := range stores {
		pending, err := s.PendingThumbnails(8)
		if err != nil {
			w.finish("thumbnails", "", err)
			continue
		}
		for _, p := range pending {
			if !w.thumbsEnabled() || len(w.requests) > 0 || w.ctx.Err() != nil {
				return processed
			}
			ctx := w.begin("thumbnails", 0)
			err := s.MakeThumbnail(ctx, dirs[i], p)
			w.mu.Lock()
			w.cancelJob()
			w.job, w.cancelJob = "", nil
			w.mu.Unlock()
			if errors.Is(err, shortvideo.ErrToolMissing) {
				w.finish("thumbnails", "", err)
				return 0
			}
			if err != nil && ctx.Err() == nil {
				w.finish("thumbnails", "", err)
				return processed
			}
			processed++
		}
	}
	return processed
}

// handleShortVideoAdmin 处理管理操作：GET 状态；POST scan/import/clean/cancel/thumbnails。
func (r *Router) handleShortVideoAdmin(w http.ResponseWriter, req *http.Request) {
	jobs := r.svJobs
	action := req.URL.Path[len("/api/media/shortvideo/admin/"):]
	if req.Method == http.MethodGet && action == "status" {
		type libStats struct {
			ID    string           `json:"library_id"`
			Name  string           `json:"name"`
			Stats shortvideo.Stats `json:"stats"`
			Error string           `json:"error,omitempty"`
		}
		var libs []libStats
		for _, lib := range r.libMgr.ListLibraries() {
			if !lib.IsOnline {
				continue
			}
			entry := libStats{ID: lib.ID, Name: lib.Name}
			if store, _, err := r.svStore(lib); err != nil {
				entry.Error = "未建立索引"
				if lib.ReadOnly {
					entry.Error = "只读库，不建立索引"
				}
			} else if entry.Stats, err = store.Stats(); err != nil {
				entry.Error = err.Error()
			}
			libs = append(libs, entry)
		}
		writeJSON(w, http.StatusOK, map[string]any{"worker": jobs.status(), "libraries": libs})
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch action {
	case "scan", "import", "clean":
		select {
		case jobs.requests <- action:
		default:
			http.Error(w, "too many queued jobs", http.StatusTooManyRequests)
			return
		}
	case "cancel":
		jobs.mu.Lock()
		if jobs.cancelJob != nil {
			jobs.cancelJob()
		}
		jobs.mu.Unlock()
	case "thumbnails":
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		jobs.mu.Lock()
		persist := jobs.persist
		jobs.mu.Unlock()
		if persist != nil {
			if err := persist(body.Enabled); err != nil {
				http.Error(w, "save setting failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		jobs.mu.Lock()
		jobs.thumbs = body.Enabled
		if !body.Enabled && jobs.job == "thumbnails" && jobs.cancelJob != nil {
			jobs.cancelJob()
		}
		jobs.mu.Unlock()
		jobs.poke()
	default:
		http.NotFound(w, req)
		return
	}
	writeJSON(w, http.StatusOK, jobs.status())
}
