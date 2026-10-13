package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"omnihub/internal/library"
	"omnihub/internal/manga"
)

// moduleDirectory 验证数据库和侧车文件仍位于正确分类，绝不使用未挂载的根路径。
func (r *Router) moduleDirectory(root, category string, names ...string) (string, error) {
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.RootPath != root {
			continue
		}
		if !lib.IsOnline {
			return "", errors.New("library offline")
		}
		dir, err := r.libMgr.ResolveCategoryPath(lib.ID+":"+category, category)
		if err != nil {
			return "", err
		}
		for _, name := range names {
			for _, suffix := range []string{"", "-wal", "-shm"} {
				if _, err := r.libMgr.ResolveCategoryPath(lib.ID+":"+category+"/"+name+suffix, category); err != nil {
					return "", err
				}
			}
		}
		return dir, nil
	}
	return "", errors.New("library not mounted")
}

type mangaItem struct {
	*manga.Manga
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	LogicalPath string `json:"logical_path"`
	ReadOnly    bool   `json:"read_only"`
}

func (r *Router) queryLibraries(w http.ResponseWriter, req *http.Request) ([]*library.Library, bool) {
	all := r.libMgr.ListLibraries()
	requested := req.URL.Query().Get("library_id")
	selected := make([]*library.Library, 0, len(all))
	for _, lib := range all {
		if requested != "" && requested != lib.ID {
			continue
		}
		if !lib.IsOnline {
			if requested != "" {
				http.Error(w, "library offline", 503)
				return nil, false
			}
			continue
		}
		selected = append(selected, lib)
	}
	if requested != "" && len(selected) == 0 {
		http.Error(w, "library not found", 404)
		return nil, false
	}
	return selected, true
}

func (r *Router) listMangaLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	items := make([]mangaItem, 0, limit+1)
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	for _, lib := range libraries {
		dir, err := r.moduleDirectory(lib.RootPath, "media_library/manga", "manga.db")
		if err != nil {
			http.Error(w, "manga storage boundary invalid", 503)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "manga.db")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			http.Error(w, "manga index unavailable", 503)
			return
		}
		db, err := r.getMangaDB(lib.RootPath)
		if err != nil {
			http.Error(w, "manga index unavailable", 503)
			return
		}
		if offset > 0 {
			count, err := db.CountMangas()
			if err != nil {
				http.Error(w, "manga count unavailable", 503)
				return
			}
			if offset >= count {
				offset -= count
				continue
			}
		}
		rows, err := db.ListMangasPage(limit+1-len(items), offset)
		if err != nil {
			http.Error(w, "manga query failed", 503)
			return
		}
		offset = 0
		for _, book := range rows {
			if !filepath.IsLocal(filepath.FromSlash(book.RelPath)) || strings.ContainsAny(book.RelPath, "\\\x00:") {
				http.Error(w, "invalid manga index path", 503)
				return
			}
			items = append(items, mangaItem{Manga: book, LibraryID: lib.ID, LibraryName: lib.Name, LogicalPath: lib.ID + ":media_library/manga/" + filepath.ToSlash(book.RelPath), ReadOnly: lib.ReadOnly})
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
