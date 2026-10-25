package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"omnihub/internal/gallery"
	"omnihub/internal/library"
)

// 相册的多个图库（见 internal/gallery/collections.go）：每个资源库的 media_library/gallery 根目录是原来的相册，
// 子文件夹各是一个图库。只放了原图分卷的图库由后台建索引后装载；NSFW 图库默认不在列表里。

// galleryCollectionFor 是取单张照片（缩略图、原图）时的图库目录：查等级（不够时写 403），再取目录。
func (r *Router) galleryCollectionFor(w http.ResponseWriter, req *http.Request, lib *library.Library, key string) (string, bool) {
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
		return "", false
	}
	if !access.galleryCollection(key) {
		http.Error(w, "forbidden: insufficient permission level", http.StatusForbidden)
		return "", false
	}
	dir, err := r.galleryCollectionDir(lib, key)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return "", false
	}
	return dir, true
}

// galleryCollectionDir 返回一个图库的目录（key 为空是根目录的相册），并检查路径不越界。
func (r *Router) galleryCollectionDir(lib *library.Library, key string) (string, error) {
	if key == "" {
		return r.moduleDirectory(lib.RootPath, "media_library/gallery", "gallery.db")
	}
	if !gallery.ValidKey(key) {
		return "", errors.New("invalid gallery collection")
	}
	root, err := r.moduleDirectory(lib.RootPath, "media_library/gallery", key+"/gallery.db")
	if err != nil {
		return "", err
	}
	return filepath.Join(root, key), nil
}

// galleryCollections 列出一个资源库的图库。
func (r *Router) galleryCollections(lib *library.Library) []gallery.Collection {
	root, err := r.moduleDirectory(lib.RootPath, "media_library/gallery")
	if err != nil {
		return nil
	}
	return gallery.Collections(root)
}

// getMetaDBAt 只读打开（并缓存）一个图库目录的 gallery.db。
func (r *Router) getMetaDBAt(dir string) (*gallery.MetaDB, error) {
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if r.closed {
		return nil, errors.New("router is closed")
	}
	if db := r.metaDB[dir]; db != nil {
		return db, nil
	}
	db, err := gallery.OpenMetaDBReadOnly(dir)
	if err != nil {
		return nil, err
	}
	r.metaDB[dir] = db
	return db, nil
}

// dropMetaDB 关掉缓存的 gallery.db（建完索引后重新打开）。
func (r *Router) dropMetaDB(dir string) {
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if db := r.metaDB[dir]; db != nil {
		_ = db.Close()
		delete(r.metaDB, dir)
	}
}

// galleryEntry 是列表里的一个图库。
type galleryEntry struct {
	lib  *library.Library
	coll gallery.Collection
	db   *gallery.MetaDB
}

// galleryEntries 返回要列出的图库：?collection= 指定一个（空值是根目录的相册），不指定时是全部非 NSFW 的，
// ?nsfw=1 时连 NSFW 的一起。只列已建好索引、当前账号等级够的。
func (r *Router) galleryEntries(w http.ResponseWriter, req *http.Request) ([]galleryEntry, bool) {
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return nil, false
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	q := req.URL.Query()
	pick, picked := q.Get("collection"), q.Has("collection")
	if picked && pick != "" && !gallery.ValidKey(pick) {
		http.Error(w, "invalid gallery collection", 400)
		return nil, false
	}
	var out []galleryEntry
	for _, lib := range libraries {
		for _, c := range r.galleryCollections(lib) {
			if !c.Indexed || (picked && c.Key != pick) || (!picked && c.NSFW && q.Get("nsfw") != "1") || !access.galleryCollection(c.Key) {
				continue
			}
			db, err := r.getMetaDBAt(c.Dir)
			if err != nil {
				http.Error(w, "gallery index unavailable", 503)
				return nil, false
			}
			out = append(out, galleryEntry{lib, c, db})
		}
	}
	return out, true
}

// galleryIndexer 在后台给只放了原图分卷的图库建索引，一次一个。
type galleryIndexer struct {
	mu        sync.Mutex
	running   bool
	current   string // 资源库名 / 图库名
	done      int
	total     int
	lastNote  string
	lastError string
	finished  time.Time
}

// startGalleryIndex 有需要建索引的图库时在后台开始（已经在跑就不重复）。
func (r *Router) startGalleryIndex() {
	ix := &r.galleryIndex
	ix.mu.Lock()
	if ix.running {
		ix.mu.Unlock()
		return
	}
	ix.running = true
	ix.mu.Unlock()
	go func() {
		defer func() {
			ix.mu.Lock()
			ix.running, ix.current = false, ""
			ix.mu.Unlock()
		}()
		for _, lib := range r.libMgr.ListLibraries() {
			if !lib.IsOnline || lib.ReadOnly {
				continue
			}
			for _, c := range r.galleryCollections(lib) {
				if !c.NeedsIndex || c.Key == "" {
					continue
				}
				ix.mu.Lock()
				ix.current, ix.done, ix.total = lib.Name+" / "+c.Name, 0, 0
				ix.mu.Unlock()
				r.dropMetaDB(c.Dir)
				res, err := gallery.IndexVolumes(context.Background(), c.Dir, func(done, total int) {
					ix.mu.Lock()
					ix.done, ix.total = done, total
					ix.mu.Unlock()
				})
				r.dropMetaDB(c.Dir)
				ix.mu.Lock()
				ix.finished = time.Now()
				ix.lastNote = lib.Name + " / " + c.Name + "：" + indexNote(res)
				ix.lastError = ""
				if err != nil {
					ix.lastError = err.Error()
				} else if len(res.Errors) > 0 {
					ix.lastError = res.Errors[0]
				}
				ix.mu.Unlock()
			}
		}
	}()
}

func indexNote(res gallery.IndexResult) string {
	note := fmt.Sprintf("新加 %d 张，已有 %d 张", res.Added, res.Known)
	if res.Unsupported > 0 {
		note += fmt.Sprintf("，%d 个不是图片", res.Unsupported)
	}
	return note
}

// handleGalleryCollections 列出各资源库的图库与张数，以及后台建索引的进度；有没建过索引的图库时顺便开始建。
func (r *Router) handleGalleryCollections(w http.ResponseWriter, req *http.Request) {
	type row struct {
		gallery.Collection
		LibraryID   string `json:"library_id"`
		LibraryName string `json:"library_name"`
		Photos      int    `json:"photos"`
	}
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
		return
	}
	rows := []row{}
	pending := false
	for _, lib := range libraries {
		for _, c := range r.galleryCollections(lib) {
			if !access.galleryCollection(c.Key) {
				continue
			}
			entry := row{Collection: c, LibraryID: lib.ID, LibraryName: lib.Name}
			if c.Indexed {
				if db, err := r.getMetaDBAt(c.Dir); err == nil {
					entry.Photos, _ = db.CountPhotos(gallery.PhotoFilter{Color: -1})
				}
			}
			pending = pending || (c.NeedsIndex && c.Key != "" && !lib.ReadOnly)
			rows = append(rows, entry)
		}
	}
	if pending {
		r.startGalleryIndex()
	}
	ix := &r.galleryIndex
	ix.mu.Lock()
	status := map[string]any{"running": ix.running, "current": ix.current, "done": ix.done, "total": ix.total,
		"last_note": ix.lastNote, "last_error": ix.lastError, "finished": ix.finished}
	ix.mu.Unlock()
	writeJSON(w, 200, map[string]any{"collections": rows, "indexing": status})
}

// ensureCollectionDir 建一个子文件夹图库（导入时用），写说明文件。
func ensureCollectionDir(root, key string, nsfw bool) (string, error) {
	dir := filepath.Join(root, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, gallery.WriteCollectionInfo(dir, key, nsfw)
}
