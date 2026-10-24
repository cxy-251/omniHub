package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"omnihub/internal/gallery"
	"omnihub/internal/library"
)

type photoItem struct {
	*gallery.Photo
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	ReadOnly    bool   `json:"read_only"`
	Collection  string `json:"collection"` // 图库（空是根目录的相册）；取缩略图、原图时带上
}

// listGalleryLibraries 跨库分页列出照片，X-Total-Count 给出总数，客户端可直接跳到任意页。
func (r *Router) listGalleryLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil || limit > 200 {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	filter, ok := galleryFilter(w, req)
	if !ok {
		return
	}
	entries, ok := r.galleryEntries(w, req)
	if !ok {
		return
	}
	total := 0
	items := make([]photoItem, 0, limit)
	for _, e := range entries {
		db := e.db
		count, err := db.CountPhotos(filter)
		if err != nil {
			http.Error(w, "gallery count unavailable", 503)
			return
		}
		total += count
		if len(items) >= limit || offset >= count {
			offset = max(0, offset-count)
			continue
		}
		photos, err := db.ListPhotos(filter, limit-len(items), offset)
		if err != nil {
			http.Error(w, "gallery query failed", 503)
			return
		}
		offset = 0
		for _, photo := range photos {
			items = append(items, photoItem{Photo: photo, LibraryID: e.lib.ID, LibraryName: e.lib.Name, ReadOnly: true, Collection: e.coll.Key})
		}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, 200, items)
}

// handleGalleryRatios 返回与列表同序的全部高宽比（×1000），客户端据此一次算出瀑布流布局。
// 10 万张约 250ms，结果按「库 + 筛选 + 数据库文件修改时间」缓存最近 4 份（每份约 0.5MB），切回相册时不再重查。
func (r *Router) handleGalleryRatios(w http.ResponseWriter, req *http.Request) {
	filter, ok := galleryFilter(w, req)
	if !ok {
		return
	}
	entries, ok := r.galleryEntries(w, req)
	if !ok {
		return
	}
	key := strconv.Itoa(filter.Color)
	for _, e := range entries {
		key += "|" + e.lib.ID + "/" + e.coll.Key
		for _, name := range []string{"gallery.db", "gallery.db-wal"} {
			if info, err := os.Stat(filepath.Join(e.coll.Dir, name)); err == nil {
				key += ":" + strconv.FormatInt(info.ModTime().UnixNano(), 36) + "/" + strconv.FormatInt(info.Size(), 36)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if data := r.galleryRatios.get(key); data != nil {
		_, _ = w.Write(data)
		return
	}
	ratios := []int{}
	for _, e := range entries {
		part, err := e.db.Ratios(filter)
		if err != nil {
			http.Error(w, "gallery index unavailable", 503)
			return
		}
		ratios = append(ratios, part...)
	}
	data, err := json.Marshal(map[string]any{"ratios": ratios})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	r.galleryRatios.put(key, data)
	_, _ = w.Write(data)
}

// ratioCache 保存最近几份高宽比响应。
type ratioCache struct {
	mu      sync.Mutex
	keys    []string
	entries map[string][]byte
}

func (c *ratioCache) get(key string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries[key]
}

func (c *ratioCache) put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string][]byte{}
	}
	if _, ok := c.entries[key]; !ok {
		c.keys = append(c.keys, key)
	}
	c.entries[key] = data
	for len(c.keys) > 4 {
		delete(c.entries, c.keys[0])
		c.keys = c.keys[1:]
	}
}

func galleryFilter(w http.ResponseWriter, req *http.Request) (gallery.PhotoFilter, bool) {
	filter := gallery.PhotoFilter{Color: -1}
	if raw := req.URL.Query().Get("color"); raw != "" {
		var err error
		if filter.Color, err = strconv.Atoi(raw); err != nil || filter.Color < 0 || filter.Color > 9 {
			http.Error(w, "invalid color", 400)
			return filter, false
		}
	}
	return filter, true
}

// handleGalleryColors 返回各色系照片数（跨库合并），用于筛选。
func (r *Router) handleGalleryColors(w http.ResponseWriter, req *http.Request) {
	entries, ok := r.galleryEntries(w, req)
	if !ok {
		return
	}
	merged := make(map[int]int)
	for _, e := range entries {
		counts, err := e.db.ColorCounts()
		if err != nil {
			http.Error(w, "gallery index unavailable", 503)
			return
		}
		for color, n := range counts {
			merged[color] += n
		}
	}
	writeJSON(w, 200, merged)
}

// galleryLibrary preserves legacy primary requests while new clients always
// identify the owning medium. It never falls back from an unknown/offline ID.
func (r *Router) galleryLibrary(w http.ResponseWriter, req *http.Request) *library.Library {
	if req.URL.Query().Get("library_id") == "" {
		lib := r.libMgr.GetPrimary()
		if lib == nil || !lib.IsOnline {
			http.Error(w, "gallery library unavailable", 404)
			return nil
		}
		return lib
	}
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return nil
	}
	return libs[0]
}
