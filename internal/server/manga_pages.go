package server

import (
	"net/http"
	"omnihub/internal/manga"
)

func (r *Router) handleMangaPageList(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page window", 400)
		return
	}
	_, _, err = r.mangaIdentity(req.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "book unavailable", 404)
		return
	}
	physical, err := r.libMgr.ResolveCategoryPath(req.URL.Query().Get("path"), "media_library/manga")
	if err != nil {
		http.Error(w, "book unavailable", 404)
		return
	}
	lease, err := r.mangaReaders.Acquire(req.Context(), physical)
	if err != nil {
		http.Error(w, "book archive unavailable", 503)
		return
	}
	defer releaseMangaReader(lease)
	pages, total, err := lease.Reader.PageWindow(offset, limit)
	if err != nil {
		http.Error(w, "invalid page window", 400)
		return
	}
	version, err := lease.Reader.Version()
	if err != nil {
		http.Error(w, "book version unavailable", 503)
		return
	}
	// 第一次取（offset 为 0）时附上卷/话列表，阅读器用它按卷跳转
	var chapters []manga.ChapterRange
	if offset == 0 {
		chapters = lease.Reader.Chapters()
	}
	writeJSON(w, 200, struct {
		Pages       []manga.PageEntry    `json:"pages"`
		Total       int                  `json:"total"`
		Offset      int                  `json:"offset"`
		FileVersion string               `json:"file_version"`
		Chapters    []manga.ChapterRange `json:"chapters,omitempty"`
	}{pages, total, offset, version, chapters})
}
