package server

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
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
	LibraryID     string `json:"library_id"`
	LibraryName   string `json:"library_name"`
	LogicalPath   string `json:"logical_path"`
	ReadOnly      bool   `json:"read_only"`
	MetadataError string `json:"metadata_error,omitempty"`
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

// listMangaLibraries 合并列出各库的漫画。以磁盘上的 CBZ 为准（索引 manga.db 是手动更新的，可能缺新放进来的书），
// 索引里有的书用索引的书名、作者等，没有的打开文件读（只读当前这一页的）。按子目录分级：
// 没有权限的子目录（如 manga/nsfw）整个不列出。可写库在这里顺便把旧的平铺目录搬成 nsfw/ 子目录。
func (r *Router) listMangaLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", 503)
		return
	}
	type entry struct {
		lib      *library.Library
		relative string
		book     *manga.Manga // 索引里没有时为 nil
	}
	var entries []entry
	for _, lib := range libraries {
		dir, err := r.moduleDirectory(lib.RootPath, "media_library/manga", "manga.db")
		if err != nil {
			http.Error(w, "manga storage boundary invalid", 503)
			return
		}
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			http.Error(w, "manga directory unavailable", 503)
			return
		}
		if !lib.ReadOnly {
			if err := r.migrateMangaLayout(req.Context(), lib, dir); err != nil {
				log.Printf("manga layout %s: %v", lib.Name, err)
			}
		}
		files, err := r.mangaCatalog.Files(req.Context(), dir)
		if err != nil {
			http.Error(w, "manga catalog unavailable or exceeds capacity", 503)
			return
		}
		indexed := map[string]*manga.Manga{}
		if _, err := os.Stat(filepath.Join(dir, "manga.db")); err == nil {
			db, err := r.getMangaDB(lib.RootPath)
			if err != nil {
				http.Error(w, "manga index unavailable", 503)
				return
			}
			books, err := db.ListAllMangas()
			if err != nil {
				http.Error(w, "manga query failed", 503)
				return
			}
			for _, book := range books {
				indexed[filepath.ToSlash(book.RelPath)] = book
			}
		}
		start := len(entries)
		for _, relative := range files {
			if !filepath.IsLocal(filepath.FromSlash(relative)) || strings.ContainsAny(relative, "\\\x00:") {
				continue
			}
			if !access.allows(lib.ID + ":media_library/manga/" + relative) {
				continue
			}
			entries = append(entries, entry{lib: lib, relative: relative, book: indexed[relative]})
		}
		title := func(e entry) string {
			if e.book != nil {
				return e.book.Title
			}
			return strings.TrimSuffix(path.Base(e.relative), path.Ext(e.relative))
		}
		part := entries[start:]
		sort.SliceStable(part, func(i, j int) bool { return title(part[i]) < title(part[j]) })
	}
	if offset > len(entries) {
		offset = len(entries)
	}
	page := entries[offset:min(len(entries), offset+limit)]
	items := make([]mangaItem, 0, len(page))
	for _, e := range page {
		logical := e.lib.ID + ":media_library/manga/" + e.relative
		if e.book != nil {
			items = append(items, mangaItem{Manga: e.book, LibraryID: e.lib.ID, LibraryName: e.lib.Name, LogicalPath: logical, ReadOnly: e.lib.ReadOnly})
			continue
		}
		select {
		case r.mediaReads <- struct{}{}:
		case <-req.Context().Done():
			return
		}
		rows, readErr := r.discoverMangaPage(req, e.lib, []string{e.relative})
		<-r.mediaReads
		if readErr != nil {
			http.Error(w, "manga metadata unavailable: "+readErr.Error(), 503)
			return
		}
		items = append(items, rows...)
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(offset+limit < len(entries)))
	writeJSON(w, http.StatusOK, items)
}

func (r *Router) discoverMangaPage(req *http.Request, lib *library.Library, paths []string) ([]mangaItem, error) {
	rows := make([]mangaItem, 0, len(paths))
	for _, relative := range paths {
		if !filepath.IsLocal(filepath.FromSlash(relative)) || strings.ContainsAny(relative, "\\\x00:") {
			return nil, errors.New("invalid discovered path")
		}
		logical := lib.ID + ":media_library/manga/" + relative
		physical, err := r.libMgr.ResolveCategoryPath(logical, "media_library/manga")
		if err != nil {
			return nil, err
		}
		book, err := r.mangaDiscovery.Metadata(req.Context(), physical, relative)
		if err != nil {
			if req.Context().Err() != nil {
				return nil, req.Context().Err()
			}
			book = &manga.Manga{ID: manga.PathIdentity(relative), Title: strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative)), RelPath: relative}
			rows = append(rows, mangaItem{Manga: book, LibraryID: lib.ID, LibraryName: lib.Name, LogicalPath: logical, ReadOnly: lib.ReadOnly, MetadataError: "书籍元数据不可用，可尝试打开或检查源文件"})
			continue
		}
		rows = append(rows, mangaItem{Manga: book, LibraryID: lib.ID, LibraryName: lib.Name, LogicalPath: logical, ReadOnly: lib.ReadOnly})
	}
	return rows, nil
}
