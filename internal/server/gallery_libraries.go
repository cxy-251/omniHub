package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"omnihub/internal/gallery"
	"omnihub/internal/library"
)

type photoItem struct {
	*gallery.Photo
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	ReadOnly    bool   `json:"read_only"`
}

// galleryDBs 返回各在线库的相册热库（没有 gallery.db 的库跳过）。
func (r *Router) galleryDBs(w http.ResponseWriter, req *http.Request) ([]*library.Library, []*gallery.MetaDB, bool) {
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return nil, nil, false
	}
	var libs []*library.Library
	var dbs []*gallery.MetaDB
	for _, lib := range libraries {
		dir, err := r.moduleDirectory(lib.RootPath, "media_library/gallery", "gallery.db")
		if err != nil {
			http.Error(w, "gallery storage boundary invalid", 503)
			return nil, nil, false
		}
		if _, err := os.Stat(filepath.Join(dir, "gallery.db")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			http.Error(w, "gallery index unavailable", 503)
			return nil, nil, false
		}
		db, err := r.getMetaDB(lib.RootPath)
		if err != nil {
			http.Error(w, "gallery index unavailable", 503)
			return nil, nil, false
		}
		libs, dbs = append(libs, lib), append(dbs, db)
	}
	return libs, dbs, true
}

// listGalleryLibraries 跨库分页列出照片，X-Total-Count 给出总数，客户端可直接跳到任意页。
func (r *Router) listGalleryLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil || limit > 200 {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	filter := gallery.PhotoFilter{Color: -1}
	if raw := req.URL.Query().Get("color"); raw != "" {
		if filter.Color, err = strconv.Atoi(raw); err != nil || filter.Color < 0 || filter.Color > 9 {
			http.Error(w, "invalid color", 400)
			return
		}
	}
	libs, dbs, ok := r.galleryDBs(w, req)
	if !ok {
		return
	}
	total := 0
	items := make([]photoItem, 0, limit)
	for i, db := range dbs {
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
			items = append(items, photoItem{Photo: photo, LibraryID: libs[i].ID, LibraryName: libs[i].Name, ReadOnly: true})
		}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, 200, items)
}

// handleGalleryColors 返回各色系照片数（跨库合并），用于筛选。
func (r *Router) handleGalleryColors(w http.ResponseWriter, req *http.Request) {
	_, dbs, ok := r.galleryDBs(w, req)
	if !ok {
		return
	}
	merged := make(map[int]int)
	for _, db := range dbs {
		counts, err := db.ColorCounts()
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
