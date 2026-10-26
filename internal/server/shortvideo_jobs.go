package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/platform"
	"omnihub/internal/shortvideo"
	"omnihub/internal/staging"
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
	detail    string // 进行中的任务现在在等什么（比如等手机重新连上）
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
	Detail     string `json:"detail,omitempty"`
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
	return svStatus{w.thumbs, w.job, w.done, w.total, w.lastJob, w.lastError, w.lastNote, w.detail}
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
	w.job, w.cancelJob, w.lastJob, w.lastNote, w.detail = "", nil, job, note, ""
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
		// 探测编码/尺寸开销小（只读文件头），始终进行，并且优先：网页端只列出探测过编码的视频
		// （浏览器放不了的要筛掉），没探测的看不到。缩略图受开关控制，还有视频没探测时每轮只做一小批。
		probed := r.probePass()
		limit := 0
		if probed > 0 {
			limit = 16
		}
		if w.thumbsEnabled() && r.thumbnailPass(limit) > 0 || probed > 0 {
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
// noSource 时去掉补充的来源库（下载落地的主库）：它只是中转，不生成缩略图、不入库图集。
func (r *Router) writableStores(noSource bool) (stores []*shortvideo.Store, dirs []string, libs []*library.Library) {
	source, _ := r.copyLibraries()
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline || lib.ReadOnly || r.libMgr.IsCategoryDisabled(shortVideoCategory) || noSource && source != nil && lib.ID == source.ID {
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
	if rest, ok := strings.CutPrefix(job, "merge\x00"); ok {
		libID, src, _ := strings.Cut(rest, "\x00")
		r.mergeShortVideo(libID, src)
		return
	}
	switch job {
	case "phone":
		r.phoneShortVideo()
		return
	case "clean-phone":
		r.cleanSyncedShortVideo()
		return
	}
	if to, ok := strings.CutPrefix(job, "copy\x00"); ok {
		r.copyShortVideo(to)
		return
	}
	stores, dirs, _ := r.writableStores(job != "scan")
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

// mergeShortVideo 把 src 文件夹里另一台设备的短视频数据库并入 libID 库（结果写进工作状态）。
func (r *Router) mergeShortVideo(libID, src string) {
	w := r.svJobs
	ctx := w.begin("merge", 1)
	lib := r.findLibrary(libID)
	if lib == nil || !lib.IsOnline || lib.ReadOnly {
		w.finish("merge", "", errors.New("目标资源库不在线或只读"))
		return
	}
	store, _, err := r.svStore(lib)
	if err != nil {
		w.finish("merge", "", err)
		return
	}
	res, err := store.MergeFrom(ctx, src, nil)
	note := fmt.Sprintf("并入图集 %d，已有相同 %d，冲突 %d（保留本库的），补作品记录 %d、封面 %d", res.Albums, res.Same, len(res.Conflicts), res.Posts, res.Thumbs)
	if len(res.Conflicts) > 0 {
		note += "；冲突：" + strings.Join(res.Conflicts[:min(3, len(res.Conflicts))], "、")
	}
	w.progress(0)
	w.finish("merge", note, err)
	w.poke()
}

// copyLibraries 返回短视频补充的方向：来源是下载落地的主库，目标是其他在线可写、有短视频目录的库。
// 只有一个库时（比如手机上）没有来源，所有库照常生成缩略图、入库图集。
func (r *Router) copyLibraries() (source *library.Library, targets []*library.Library) {
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline {
			continue
		}
		if lib.IsPrimary {
			source = lib
		} else if !lib.ReadOnly {
			if _, err := r.moduleDirectory(lib.RootPath, shortVideoCategory, shortvideo.DatabaseFiles...); err == nil {
				targets = append(targets, lib)
			}
		}
	}
	if source == nil || len(targets) == 0 {
		return nil, nil
	}
	return source, targets
}

// copyEnds 找出补充到 toID 库的两端：返回来源和目标的 shortvideo/ 目录与目标库。
func (r *Router) copyEnds(toID string) (src, dst string, store *shortvideo.Store, err error) {
	source, targets := r.copyLibraries()
	var to *library.Library
	for _, t := range targets {
		if t.ID == toID {
			to = t
		}
	}
	if to == nil {
		return "", "", nil, errors.New("目标资源库不在线、只读，或不是可补充的库")
	}
	if src, err = r.moduleDirectory(source.RootPath, shortVideoCategory, shortvideo.DatabaseFiles...); err != nil {
		return
	}
	store, dst, err = r.svStore(to)
	return
}

// copyShortVideo 把来源库的短视频增量补到 toID 库已有的博主（见 shortvideo.PlanCopy），
// 再并入这些博主已入库的图集，最后重新扫描目标库。
func (r *Router) copyShortVideo(toID string) {
	w := r.svJobs
	src, dst, store, err := r.copyEnds(toID)
	if err != nil {
		w.finish("copy", "", err)
		return
	}
	plan, err := shortvideo.PlanCopy(src, dst, true, staging.BrowserPlatforms)
	if err != nil {
		w.finish("copy", "", err)
		return
	}
	ctx := w.begin("copy", plan.Count)
	res, err := shortvideo.RunCopy(ctx, src, dst, plan, func(int, int64) { w.progress(0) })
	note := fmt.Sprintf("复制 %d 个文件（%.1f GB）", res.Copied, float64(res.Bytes)/1e9)
	if len(plan.Conflicts) > 0 {
		note += fmt.Sprintf("；%d 个同名文件大小不同，没有覆盖：%s", len(plan.Conflicts), strings.Join(plan.Conflicts[:min(3, len(plan.Conflicts))], "、"))
	}
	if err == nil {
		var merged shortvideo.MergeResult
		if merged, err = store.MergeFrom(ctx, src, plan.Keeps); err == nil && merged.Albums > 0 {
			note += fmt.Sprintf("，并入已入库图集 %d", merged.Albums)
		}
	}
	if err == nil {
		var st shortvideo.ScanStats
		if st, err = store.Scan(ctx, dst); err == nil {
			note += fmt.Sprintf("；扫描新增 %d", st.Added)
		}
	}
	if errors.Is(err, context.Canceled) {
		note, err = "已取消；"+note, nil
	}
	w.finish("copy", note, err)
	w.poke()
}

// ConfigurePhone 设置 adb 位置与手机上 shortvideo/ 的路径。
func (r *Router) ConfigurePhone(adb, dir string) {
	r.phoneADB, r.phoneDir = adb, dir
}

// phoneEnds 连上手机，返回来源库（SSD）的 shortvideo/ 目录、来源库与手机。
func (r *Router) phoneEnds(ctx context.Context) (string, *library.Library, shortvideo.Phone, error) {
	source, _ := r.copyLibraries()
	if source == nil {
		for _, lib := range r.libMgr.ListLibraries() {
			if lib.IsPrimary && lib.IsOnline && !lib.ReadOnly {
				source = lib
			}
		}
	}
	if source == nil {
		return "", nil, shortvideo.Phone{}, errors.New("主资源库不在线")
	}
	if r.phoneADB == "" {
		return "", nil, shortvideo.Phone{}, errors.New("没找到 adb（Android platform-tools）")
	}
	src, err := r.moduleDirectory(source.RootPath, shortVideoCategory, shortvideo.DatabaseFiles...)
	if err != nil {
		return "", nil, shortvideo.Phone{}, err
	}
	phone, err := shortvideo.ConnectADB(ctx, r.phoneADB, r.phoneDir)
	return src, source, phone, err
}

// keepDirs 是清理 SSD 时还要核对的库（SD 卡等补充目标）的 shortvideo/ 目录。
func (r *Router) keepDirs() []string {
	_, targets := r.copyLibraries()
	var dirs []string
	for _, t := range targets {
		if d, err := r.moduleDirectory(t.RootPath, shortVideoCategory, shortvideo.DatabaseFiles...); err == nil {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// phoneShortVideo 把 SSD 上插件平台的短视频全部增量补到手机（见 shortvideo.RunPhone）。
func (r *Router) phoneShortVideo() {
	w := r.svJobs
	ctx := w.begin("phone", 0)
	src, _, phone, err := r.phoneEnds(ctx)
	if err != nil {
		w.finish("phone", "", err)
		return
	}
	phone.Waiting = func(note string) {
		w.mu.Lock()
		w.detail = note
		w.mu.Unlock()
	}
	plan, listing, err := shortvideo.PlanPhone(ctx, src, phone, staging.BrowserPlatforms)
	if err != nil {
		w.finish("phone", "", err)
		return
	}
	w.mu.Lock()
	w.total = plan.Count
	w.mu.Unlock()
	res, err := shortvideo.RunPhone(ctx, src, phone, plan, listing, func(done int, _ int64) {
		w.mu.Lock()
		w.done = done
		w.mu.Unlock()
	})
	note := fmt.Sprintf("复制 %d 个文件（%.1f GB）", res.Copied, float64(res.Bytes)/1e9)
	if len(plan.Conflicts) > 0 {
		note += fmt.Sprintf("；%d 个同名文件大小不同，没有覆盖：%s", len(plan.Conflicts), strings.Join(plan.Conflicts[:min(3, len(plan.Conflicts))], "、"))
	}
	if errors.Is(err, context.Canceled) {
		note, err = "已取消；"+note, nil
	}
	w.finish("phone", note, err)
	w.poke()
}

// cleanSyncedShortVideo 把 SSD 上已同步到手机（和 SD 卡已有博主）的视频与图集移进回收站，保留下载记录。
func (r *Router) cleanSyncedShortVideo() {
	w := r.svJobs
	ctx := w.begin("clean-phone", 0)
	src, source, phone, err := r.phoneEnds(ctx)
	if err != nil {
		w.finish("clean-phone", "", err)
		return
	}
	listing, err := phone.List(ctx)
	if err != nil {
		w.finish("clean-phone", "", err)
		return
	}
	plan, err := shortvideo.PlanClean(src, listing, r.keepDirs(), staging.BrowserPlatforms)
	if err != nil {
		w.finish("clean-phone", "", err)
		return
	}
	w.mu.Lock()
	w.total = len(plan.Items)
	w.mu.Unlock()
	n, err := shortvideo.RunClean(ctx, plan, platform.Trash, func(int) { w.progress(0) })
	note := fmt.Sprintf("移进回收站 %d 项（%d 个文件，%.1f GB）", n, plan.Files, float64(plan.Bytes)/1e9)
	if plan.NotOnPhone+plan.NotOnKeep > 0 {
		note += fmt.Sprintf("；保留手机上没有的 %d 个、SD 卡还没补的 %d 个", plan.NotOnPhone, plan.NotOnKeep)
	}
	if errors.Is(err, context.Canceled) {
		note, err = "已取消；"+note, nil
	}
	if store, dir, serr := r.svStore(source); serr == nil {
		_, _ = store.Scan(ctx, dir)
	}
	w.finish("clean-phone", note, err)
	w.poke()
}

// thumbnailPass 为各库生成一批缩略图（limit 大于 0 时最多这么多个），返回处理数量；有显式任务等待或开关关闭时提前返回。
func (r *Router) thumbnailPass(limit int) int {
	w := r.svJobs
	stores, dirs, _ := r.writableStores(true)
	processed := 0
	for i, s := range stores {
		// 每次取一批要把全部作品和缩略图记录读一遍，手机上有 10 万个作品，批小了会反复全表读取
		pending, err := s.PendingThumbnails(256)
		if err != nil {
			w.finish("thumbnails", "", err)
			continue
		}
		for len(pending) > 0 {
			if limit > 0 && processed >= limit || !w.thumbsEnabled() || len(w.requests) > 0 || w.ctx.Err() != nil || !r.coolDown("缩略图", w.thumbsEnabled) {
				return processed
			}
			batch := pending[:min(len(pending), parallelism())]
			pending = pending[len(batch):]
			ctx := w.begin("thumbnails", 0)
			errs := make([]error, len(batch))
			var wg sync.WaitGroup
			for k, p := range batch {
				wg.Add(1)
				go func() { defer wg.Done(); errs[k] = s.MakeThumbnail(ctx, dirs[i], p) }()
			}
			wg.Wait()
			w.mu.Lock()
			w.cancelJob()
			w.job, w.cancelJob = "", nil
			w.mu.Unlock()
			for _, err := range errs {
				if errors.Is(err, shortvideo.ErrToolMissing) {
					w.finish("thumbnails", "", err)
					return 0
				}
				if err != nil && ctx.Err() == nil {
					w.finish("thumbnails", "", err)
					return processed
				}
			}
			processed += len(batch)
		}
	}
	return processed
}

// 后台任务（缩略图、读取视频信息）的温度阈值（°C）：机身达到 hotTemp 时暂停，降到 resumeTemp 以下再继续。
// 数值沿用 z6x hub 在这台手机上用的（36～40°C，暂停后降到 37°C 继续）。
const (
	hotTemp    = 40
	resumeTemp = 37
)

// parallelism 是后台同时跑几个 ffprobe / ffmpeg：手机电池低于 36°C 时 3 个（8 核大多闲着，z6x hub 也是这么跑的），
// 否则 1 个。读不到温度的设备（Deck，可能正在玩游戏）一直 1 个。
func parallelism() int {
	if t, ok := platform.DeviceTemp(); ok && t < 36 {
		return 3
	}
	return 1
}

// coolDown 在机身过热时暂停后台任务 what（手机上用；读不到温度的设备如 Deck 不受影响），
// 暂停期间每 5 秒看一次温度。返回 false 表示等待时来了别的任务、still 返回 false 或服务在退出。
func (r *Router) coolDown(what string, still func() bool) bool {
	w := r.svJobs
	t, ok := platform.DeviceTemp()
	if !ok || t < hotTemp {
		return true
	}
	defer func() {
		w.mu.Lock()
		w.detail = ""
		w.mu.Unlock()
	}()
	for ok && t >= resumeTemp {
		w.mu.Lock()
		w.detail = fmt.Sprintf("机身 %.1f°C，%s暂停，降到 %d°C 以下继续", t, what, resumeTemp)
		w.mu.Unlock()
		select {
		case <-w.ctx.Done():
			return false
		case <-time.After(5 * time.Second):
		}
		if len(w.requests) > 0 || !still() {
			return false
		}
		t, ok = platform.DeviceTemp()
	}
	return true
}

// probePass 为各库探测一小批视频的编码、尺寸与时长；有显式任务等待时提前返回。
func (r *Router) probePass() int {
	w := r.svJobs
	stores, dirs, _ := r.writableStores(false)
	processed := 0
	for i, s := range stores {
		pending, err := s.PendingProbes(64)
		if err != nil {
			continue
		}
		for len(pending) > 0 {
			if len(w.requests) > 0 || w.ctx.Err() != nil || !r.coolDown("读取视频信息", func() bool { return true }) {
				return processed
			}
			batch := pending[:min(len(pending), parallelism())]
			pending = pending[len(batch):]
			errs := make([]error, len(batch))
			var wg sync.WaitGroup
			for k, p := range batch {
				wg.Add(1)
				go func() { defer wg.Done(); errs[k] = s.ProbeVideo(w.ctx, dirs[i], p) }()
			}
			wg.Wait()
			for _, err := range errs {
				if errors.Is(err, shortvideo.ErrToolMissing) {
					w.finish("probe", "", err)
					return 0
				}
				if err != nil {
					return processed
				}
			}
			processed += len(batch)
		}
	}
	return processed
}

// handleShortVideoAdmin 处理管理操作：GET 状态；POST scan/import/clean/merge/copy-plan/copy/phone-plan/phone/clean-plan/clean-phone/cancel/thumbnails。
func (r *Router) handleShortVideoAdmin(w http.ResponseWriter, req *http.Request) {
	jobs := r.svJobs
	action := req.URL.Path[len("/api/media/shortvideo/admin/"):]
	if req.Method == http.MethodGet && action == "status" {
		type libStats struct {
			ID     string           `json:"library_id"`
			Name   string           `json:"name"`
			Source bool             `json:"copy_source"` // 补充的来源（下载落地的主库）：不生成缩略图、不入库图集
			Stats  shortvideo.Stats `json:"stats"`
			Error  string           `json:"error,omitempty"`
		}
		type libRef struct {
			ID   string `json:"library_id"`
			Name string `json:"name"`
		}
		var libs []libStats
		var copyTo []libRef
		source, targets := r.copyLibraries()
		for _, t := range targets {
			copyTo = append(copyTo, libRef{t.ID, t.Name})
		}
		for _, lib := range r.libMgr.ListLibraries() {
			if !lib.IsOnline {
				continue
			}
			entry := libStats{ID: lib.ID, Name: lib.Name, Source: source != nil && lib.ID == source.ID}
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
		writeJSON(w, http.StatusOK, map[string]any{"worker": jobs.status(), "libraries": libs, "copy_targets": copyTo})
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
	case "merge":
		// 合并另一台设备拷来的数据库：路径是本机文件夹，只接受本机客户端。
		if !auth.IsTrustedLocalFromContext(req.Context()) {
			http.Error(w, "只能在 Deck 本机合并", http.StatusForbidden)
			return
		}
		var body struct {
			LibraryID string `json:"library_id"`
			Path      string `json:"path"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.LibraryID == "" || !filepath.IsAbs(body.Path) || strings.ContainsRune(body.Path+body.LibraryID, 0) {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		select {
		case jobs.requests <- "merge\x00" + body.LibraryID + "\x00" + body.Path:
		default:
			http.Error(w, "too many queued jobs", http.StatusTooManyRequests)
			return
		}
	case "copy-plan", "copy":
		// 把来源库（SSD）的短视频补到目标库已有的博主：copy-plan 只列出要做的事，copy 排队执行。
		var body struct {
			To string `json:"to"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || strings.ContainsRune(body.To, 0) {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		src, dst, _, err := r.copyEnds(body.To)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if action == "copy-plan" {
			plan, err := shortvideo.PlanCopy(src, dst, true, staging.BrowserPlatforms)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, plan)
			return
		}
		select {
		case jobs.requests <- "copy\x00" + body.To:
		default:
			http.Error(w, "too many queued jobs", http.StatusTooManyRequests)
			return
		}
	case "phone-plan", "clean-plan":
		// 连手机读清单要一会儿（几万个文件时十几秒）；只列出要做的事，不改任何东西
		src, _, phone, err := r.phoneEnds(req.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if action == "phone-plan" {
			plan, _, err := shortvideo.PlanPhone(req.Context(), src, phone, staging.BrowserPlatforms)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			writeJSON(w, http.StatusOK, plan)
			return
		}
		listing, err := phone.List(req.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		plan, err := shortvideo.PlanClean(src, listing, r.keepDirs(), staging.BrowserPlatforms)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, plan)
		return
	case "phone", "clean-phone":
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
