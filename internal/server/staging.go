package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/gallery"
	"omnihub/internal/library"
	"omnihub/internal/platform"
	"omnihub/internal/staging"
)

// 暂存区（各资源库根目录的 staging/）：只给桌面端用，路径是本机文件，所以只接受本机客户端。
//
//	GET  /api/staging/list                         各库的批次
//	GET  /api/staging/files?library_id&batch&kind&offset&limit
//	POST /api/staging/delete {library_id,batch,paths?}   移到回收站；不给 paths 时整个批次
//	POST /api/staging/collect {library_id}               把 Chrome 下载目录里插件下好的短视频收进新批次（后台）
//	POST /api/staging/import {library_id,batch}          移入资源库（后台），随后扫描涉及的分类
//	POST /api/staging/cancel                             取消后台任务
//	GET  /api/staging/status                             后台任务状态
type stagingWorker struct {
	mu       sync.Mutex
	job      string
	library  string
	done     int
	total    int
	cancel   context.CancelFunc
	last     string
	lastNote string
	lastErr  string
	finished time.Time
}

func (r *Router) handleStaging(w http.ResponseWriter, req *http.Request) {
	if !auth.IsTrustedLocalFromContext(req.Context()) {
		http.Error(w, "暂存区只在 Deck 本机的桌面端使用", http.StatusForbidden)
		return
	}
	action := strings.TrimPrefix(req.URL.Path, "/api/staging/")
	if req.Method == http.MethodGet {
		switch action {
		case "list":
			r.stagingList(w)
		case "files":
			r.stagingFiles(w, req)
		case "status":
			writeJSON(w, http.StatusOK, r.staging.status())
		default:
			http.NotFound(w, req)
		}
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		LibraryID string   `json:"library_id"`
		Batch     string   `json:"batch"`
		Paths     []string `json:"paths"`
	}
	if action != "cancel" {
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
	}
	var lib *library.Library
	if action != "cancel" {
		if lib = r.findLibrary(in.LibraryID); lib == nil || !lib.IsOnline || lib.ReadOnly {
			http.Error(w, "资源库不在线或只读", http.StatusConflict)
			return
		}
	}
	switch action {
	case "delete":
		var targets []string
		if len(in.Paths) == 0 {
			dir, err := staging.BatchDir(lib.RootPath, in.Batch)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			targets = []string{dir}
		}
		for _, rel := range in.Paths {
			p, err := staging.Resolve(lib.RootPath, in.Batch, rel)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			targets = append(targets, p)
		}
		failed := 0
		for _, p := range targets {
			if err := platform.Trash(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				failed++
			}
		}
		writeJSON(w, http.StatusOK, map[string]int{"trashed": len(targets) - failed, "failed": failed})
	case "collect", "import":
		if !r.staging.start(action, lib, func(ctx context.Context, progress func(int, int)) (string, error) {
			if action == "collect" {
				return r.stagingCollect(ctx, lib, progress)
			}
			return r.stagingImport(ctx, lib, in.Batch, progress)
		}) {
			http.Error(w, "已有暂存区任务在进行", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, r.staging.status())
	case "cancel":
		r.staging.mu.Lock()
		if r.staging.cancel != nil {
			r.staging.cancel()
		}
		r.staging.mu.Unlock()
		writeJSON(w, http.StatusOK, r.staging.status())
	default:
		http.NotFound(w, req)
	}
}

func (r *Router) stagingList(w http.ResponseWriter) {
	type libBatches struct {
		ID      string          `json:"library_id"`
		Name    string          `json:"name"`
		Batches []staging.Batch `json:"batches"`
		Error   string          `json:"error,omitempty"`
		// NearDownloads：与 Chrome 下载目录在同一块盘上，收件只是改名；否则要复制（大量视频会很慢）。
		NearDownloads bool `json:"near_downloads"`
	}
	home, _ := os.UserHomeDir()
	out := []libBatches{}
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline || lib.ReadOnly {
			continue
		}
		entry := libBatches{ID: lib.ID, Name: lib.Name, NearDownloads: staging.SameDisk(lib.RootPath, filepath.Join(home, "Downloads"))}
		var err error
		if entry.Batches, err = staging.List(lib.RootPath); err != nil {
			entry.Error = err.Error()
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

func (r *Router) stagingFiles(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	lib := r.findLibrary(q.Get("library_id"))
	if lib == nil || !lib.IsOnline {
		http.Error(w, "资源库不在线", http.StatusNotFound)
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	files, total, err := staging.Files(lib.RootPath, q.Get("batch"), q.Get("kind"), max(0, offset), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, files)
}

// stagingCollect 收 Chrome 下载目录（~/Downloads）里插件下好的短视频。
func (r *Router) stagingCollect(ctx context.Context, lib *library.Library, progress func(int, int)) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	res, err := staging.CollectBrowser(ctx, filepath.Join(home, "Downloads"), lib.RootPath, progress)
	parts := []string{fmt.Sprintf("收进 %d 个作品（%d 个文件，%d 位作者）", res.Moved, res.Files, len(res.Authors))}
	if errors.Is(err, staging.ErrNothing) {
		parts, err = []string{"没有可收的作品"}, nil
	}
	if res.Busy > 0 {
		parts = append(parts, fmt.Sprintf("%d 个还在下载，下次再收", res.Busy))
	}
	if res.Loose > 0 {
		parts = append(parts, fmt.Sprintf("下载目录根下有 %d 个没有作者的视频（「下载 (N).mp4」），没动", res.Loose))
	}
	if len(res.Errors) > 0 {
		parts = append(parts, fmt.Sprintf("%d 个出错：%s", len(res.Errors), res.Errors[0]))
	}
	return strings.Join(parts, "；"), err
}

// stagingImport 把批次移入资源库；移入了短视频就排一次扫描。
func (r *Router) stagingImport(ctx context.Context, lib *library.Library, batch string, progress func(int, int)) (string, error) {
	res, err := staging.Import(ctx, lib.RootPath, batch, func(ctx context.Context, dir string) (string, error) {
		return r.stagingGallery(ctx, lib, dir)
	}, progress)
	for _, area := range res.Areas {
		if area == shortVideoCategory {
			select {
			case r.svJobs.requests <- "scan":
			default:
			}
		}
	}
	note := res.Describe()
	if len(res.Errors) > 0 {
		note += "：" + res.Errors[0]
	}
	return note, err
}

func (s *stagingWorker) start(job string, lib *library.Library, run func(context.Context, func(int, int)) (string, error)) bool {
	s.mu.Lock()
	if s.job != "" {
		s.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.job, s.library, s.done, s.total, s.cancel = job, lib.Name, 0, 0, cancel
	s.mu.Unlock()
	go func() {
		note, err := run(ctx, func(done, total int) {
			s.mu.Lock()
			s.done, s.total = done, total
			s.mu.Unlock()
		})
		s.mu.Lock()
		s.last, s.lastNote, s.lastErr, s.finished = s.job, note, "", time.Now()
		if err != nil {
			s.lastErr = err.Error()
			if ctx.Err() != nil {
				s.lastErr = "已取消"
			}
		}
		s.job, s.cancel = "", nil
		s.mu.Unlock()
		cancel()
	}()
	return true
}

func (s *stagingWorker) status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"job": s.job, "library": s.library, "done": s.done, "total": s.total,
		"last_job": s.last, "last_note": s.lastNote, "last_error": s.lastErr, "finished": s.finished}
}

// stagingGallery 把批次 gallery/ 里的图片并进资源库的相册（gallery.db，按内容去重）：
// gallery/<图库名>/ 下的进同名图库（没有时新建；批次里带了 collection.json 的照抄，如 X 标了 NSFW），
// 直接放在 gallery/ 下的进根目录的相册。没有出错时删掉已入库（或相册里本来就有）的图片；其余（视频等不支持的）留在批次里。
func (r *Router) stagingGallery(ctx context.Context, lib *library.Library, dir string) (string, error) {
	galleryDir, err := r.moduleDirectory(lib.RootPath, "media_library/gallery", "gallery.db")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(galleryDir, 0o755); err != nil {
		return "", err
	}
	type part struct{ src, target, name string }
	var parts []part
	entries, _ := os.ReadDir(dir)
	loose := false
	for _, e := range entries {
		switch {
		case e.IsDir() && gallery.ValidKey(e.Name()):
			src := filepath.Join(dir, e.Name())
			nsfw := false
			if data, err := os.ReadFile(filepath.Join(src, gallery.CollectionFile)); err == nil {
				var info struct {
					NSFW bool `json:"nsfw"`
				}
				_ = json.Unmarshal(data, &info)
				nsfw = info.NSFW
			}
			target, err := ensureCollectionDir(galleryDir, e.Name(), nsfw)
			if err != nil {
				return "", err
			}
			_ = os.Remove(filepath.Join(src, gallery.CollectionFile))
			r.dropMetaDB(target)
			parts = append(parts, part{src, target, e.Name()})
		case e.Type().IsRegular():
			loose = true
		}
	}
	notes := []string{}
	for _, p := range parts {
		res, err := gallery.ImportDirectory(ctx, p.target, p.src, nil)
		r.dropMetaDB(p.target)
		if err != nil {
			return strings.Join(notes, "；"), err
		}
		notes = append(notes, p.name+" "+galleryImportNote(res))
		if len(res.Errors) == 0 {
			removeImportedImages(p.src)
		}
	}
	if loose { // 直接放在 gallery/ 下的（不含子文件夹）
		tmp := filepath.Join(dir, ".loose")
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			return "", err
		}
		for _, e := range entries {
			if e.Type().IsRegular() {
				_ = os.Rename(filepath.Join(dir, e.Name()), filepath.Join(tmp, e.Name()))
			}
		}
		res, err := gallery.ImportDirectory(ctx, galleryDir, tmp, nil)
		r.dropMetaDB(galleryDir)
		for _, e := range entries { // 放回原处（剩下的留在批次里）
			if e.Type().IsRegular() {
				_ = os.Rename(filepath.Join(tmp, e.Name()), filepath.Join(dir, e.Name()))
			}
		}
		_ = os.Remove(tmp)
		if err != nil {
			return strings.Join(notes, "；"), err
		}
		notes = append(notes, "相册 "+galleryImportNote(res))
		if len(res.Errors) == 0 {
			removeImportedImages(dir)
		}
	}
	return strings.Join(notes, "；"), nil
}

func galleryImportNote(res gallery.ImportProgress) string {
	note := fmt.Sprintf("新增 %d 张，已有 %d 张", res.Added, res.Duplicates)
	if res.Unsupported > 0 {
		note += fmt.Sprintf("，%d 个不是图片（留在暂存区）", res.Unsupported)
	}
	if len(res.Errors) > 0 {
		note += fmt.Sprintf("，%d 张出错（图片都留在暂存区）：%s", len(res.Errors), res.Errors[0])
	}
	return note
}

// removeImportedImages 删掉目录里已入库的图片（HEIC、AVIF 相册不收，留着）。
func removeImportedImages(dir string) {
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && staging.Kind(p) == "image" && !strings.EqualFold(filepath.Ext(p), ".heic") && !strings.EqualFold(filepath.Ext(p), ".avif") {
			_ = os.Remove(p)
		}
		return nil
	})
}
