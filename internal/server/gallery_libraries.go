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

func (r *Router) listGalleryLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	includeDeleted := req.URL.Query().Get("include_deleted") == "1"
	items := make([]photoItem, 0, limit+1)
	for _, lib := range libraries {
		dir, err := r.moduleDirectory(lib.RootPath, "media_library/gallery", "gallery.db")
		if err != nil {
			http.Error(w, "gallery storage boundary invalid", 503)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "gallery.db")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			http.Error(w, "gallery index unavailable", 503)
			return
		}
		db, err := r.getMetaDB(lib.RootPath)
		if err != nil {
			http.Error(w, "gallery index unavailable", 503)
			return
		}
		if offset > 0 {
			count, err := db.CountPhotos(includeDeleted)
			if err != nil {
				http.Error(w, "gallery count unavailable", 503)
				return
			}
			if offset >= count {
				offset -= count
				continue
			}
		}
		photos, err := db.ListPhotos(includeDeleted, limit+1-len(items), offset)
		if err != nil {
			http.Error(w, "gallery query failed", 503)
			return
		}
		offset = 0
		for _, photo := range photos {
			items = append(items, photoItem{Photo: photo, LibraryID: lib.ID, LibraryName: lib.Name, ReadOnly: lib.ReadOnly})
		}
		if len(items) > limit {
			break
		}
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(more))
	writeJSON(w, 200, items)
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
