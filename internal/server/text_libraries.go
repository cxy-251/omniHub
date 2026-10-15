package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

type textItem struct {
	Name        string `json:"name"`
	RelPath     string `json:"rel_path"`
	Ext         string `json:"ext"`
	Size        int64  `json:"size"`
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
}

func pageBounds(req *http.Request) (limit, offset int, err error) {
	limit = 100
	if raw := req.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return
		}
	}
	if raw := req.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil {
			return
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		err = errors.New("invalid page bounds")
	}
	return
}

func (r *Router) listTextLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", http.StatusBadRequest)
		return
	}
	items := make([]textItem, 0, limit+1)
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", 503)
		return
	}
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	for _, lib := range libraries {
		for _, category := range []string{"media_library/docs", "media_library/novels"} {
			if r.libMgr.IsCategoryDisabled(category) {
				continue
			}
			root, err := r.libMgr.ResolveCategoryPath(lib.ID+":"+category, category)
			if err != nil {
				http.Error(w, "text storage boundary invalid", 503)
				return
			}
			info, err := os.Lstat(root)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !info.IsDir() {
				http.Error(w, "text storage unavailable", 503)
				return
			}
			paths, total, err := r.textCatalog.Page(req.Context(), root, limit+1-len(items), offset)
			if err != nil {
				http.Error(w, "text catalog unavailable or exceeds capacity", 503)
				return
			}
			if offset >= total {
				offset -= total
				continue
			}
			offset = 0
			for _, path := range paths {
				logical := lib.ID + ":" + category + "/" + filepath.ToSlash(path)
				if !access.allows(logical) {
					continue
				}
				physical, err := r.libMgr.ResolveCategoryPath(logical, category)
				if err != nil {
					http.Error(w, "text resource boundary changed", 503)
					return
				}
				info, err := os.Lstat(physical)
				if errors.Is(err, os.ErrNotExist) {
					r.textCatalog.Invalidate(root)
					continue
				}
				if err != nil || !info.Mode().IsRegular() {
					http.Error(w, "text resource unavailable", 503)
					return
				}
				items = append(items, textItem{Name: filepath.Base(path), RelPath: logical, Ext: filepath.Ext(path), Size: info.Size(), LibraryID: lib.ID, LibraryName: lib.Name})
			}
			if len(items) > limit {
				break
			}
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
	writeJSON(w, http.StatusOK, items)
}
