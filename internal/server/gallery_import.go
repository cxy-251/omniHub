package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/gallery"
)

// galleryImporter 把 Deck 上的一个文件夹导入某个资源库的相册（gallery.db + originals 分卷）。同一时间只跑一个。
type galleryImporter struct {
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	source   string
	library  string
	progress gallery.ImportProgress
	err      string
	finished time.Time
}

type galleryImportStatus struct {
	Running  bool                   `json:"running"`
	Source   string                 `json:"source"`
	Library  string                 `json:"library"`
	Progress gallery.ImportProgress `json:"progress"`
	Error    string                 `json:"error,omitempty"`
	Finished time.Time              `json:"finished,omitzero"`
}

// handleGalleryImport：GET 状态；POST {"library_id","path"} 开始导入；DELETE 取消。
// 路径是本机文件夹，所以只接受本机客户端。
func (r *Router) handleGalleryImport(w http.ResponseWriter, req *http.Request) {
	if !auth.IsTrustedLocalFromContext(req.Context()) {
		http.Error(w, "只能在 Deck 本机导入", http.StatusForbidden)
		return
	}
	im := &r.galleryImport
	switch req.Method {
	case http.MethodGet:
	case http.MethodDelete:
		im.mu.Lock()
		if im.cancel != nil {
			im.cancel()
		}
		im.mu.Unlock()
	case http.MethodPost:
		var in struct {
			LibraryID string `json:"library_id"`
			Path      string `json:"path"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		lib := r.findLibrary(in.LibraryID)
		if lib == nil || !lib.IsOnline {
			http.Error(w, "资源库不在线", http.StatusNotFound)
			return
		}
		if lib.ReadOnly {
			http.Error(w, "资源库是只读的", http.StatusConflict)
			return
		}
		if info, err := os.Stat(in.Path); err != nil || !info.IsDir() || !filepath.IsAbs(in.Path) {
			http.Error(w, "不是文件夹："+in.Path, http.StatusBadRequest)
			return
		}
		dir, err := r.moduleDirectory(lib.RootPath, "media_library/gallery", "gallery.db")
		if err != nil {
			http.Error(w, "相册目录无效："+err.Error(), http.StatusServiceUnavailable)
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		im.mu.Lock()
		if im.running {
			im.mu.Unlock()
			http.Error(w, "已有导入在进行", http.StatusConflict)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		im.running, im.cancel, im.source, im.library = true, cancel, in.Path, lib.Name
		im.progress, im.err, im.finished = gallery.ImportProgress{}, "", time.Time{}
		im.mu.Unlock()
		go func() {
			result, err := gallery.ImportDirectory(ctx, dir, in.Path, func(p gallery.ImportProgress) {
				im.mu.Lock()
				im.progress = p
				im.mu.Unlock()
			})
			im.mu.Lock()
			im.running, im.cancel, im.progress, im.finished = false, nil, result, time.Now()
			if err != nil {
				im.err = err.Error()
				if ctx.Err() != nil {
					im.err = "已取消"
				}
			}
			im.mu.Unlock()
			cancel()
		}()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	im.mu.Lock()
	status := galleryImportStatus{im.running, im.source, im.library, im.progress, im.err, im.finished}
	im.mu.Unlock()
	writeJSON(w, http.StatusOK, status)
}
