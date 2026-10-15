package server

import (
	"errors"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"omnihub/internal/auth"
	"omnihub/internal/text"
)

const novelCategory = "media_library/novels"

// novel 是书架上的一本书。ID 为 <库ID>:media_library/novels/<相对路径>，与阅读进度、EPUB 接口共用。
type novel struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Category  string `json:"category"`
	Format    string `json:"format"`
	LibraryID string `json:"library_id"`
}

// novelCategoryOf 取相对路径的前两级目录作分类（如 "standard/古腾堡"）；直接放在根下的书归入上一级。
func novelCategoryOf(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) > 2 {
		return parts[0] + "/" + parts[1]
	}
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// allNovels 合并各在线库的小说清单（隐藏目录除外），按分类、书名排序。
func (r *Router) allNovels(w http.ResponseWriter, req *http.Request) ([]novel, bool) {
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return nil, false
	}
	if r.libMgr.IsCategoryDisabled(novelCategory) {
		return nil, true
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	var all []novel
	for _, lib := range libs {
		root, err := r.libMgr.ResolveCategoryPath(lib.ID+":"+novelCategory, novelCategory)
		if err != nil {
			continue
		}
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		}
		paths, err := r.novelCatalog.SnapshotPaths(req.Context(), root)
		if err != nil {
			http.Error(w, "novel catalog unavailable", http.StatusServiceUnavailable)
			return nil, false
		}
		for _, p := range paths {
			p = strings.ReplaceAll(p, "\\", "/")
			if strings.HasPrefix(p, ".") || strings.Contains(p, "/.") {
				continue
			}
			ext := path.Ext(p)
			if !access.allows(lib.ID + ":" + novelCategory + "/" + p) {
				continue // 等级不足的子分类（如 nsfw）不出现在书架上
			}
			all = append(all, novel{
				ID: lib.ID + ":" + novelCategory + "/" + p, Title: strings.TrimSuffix(path.Base(p), ext),
				Category: novelCategoryOf(p), Format: strings.TrimPrefix(strings.ToLower(ext), "."), LibraryID: lib.ID,
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Category != all[j].Category {
			return all[i].Category < all[j].Category
		}
		return all[i].Title < all[j].Title
	})
	return all, true
}

// handleNovelCategories 返回分类及书数。
func (r *Router) handleNovelCategories(w http.ResponseWriter, req *http.Request) {
	all, ok := r.allNovels(w, req)
	if !ok {
		return
	}
	type entry struct {
		Category string `json:"category"`
		Count    int    `json:"count"`
	}
	var out []entry
	for _, n := range all {
		if len(out) == 0 || out[len(out)-1].Category != n.Category {
			out = append(out, entry{Category: n.Category})
		}
		out[len(out)-1].Count++
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNovels 按分类分页列出书目，X-Total-Count 为总数。
func (r *Router) handleNovels(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", http.StatusBadRequest)
		return
	}
	all, ok := r.allNovels(w, req)
	if !ok {
		return
	}
	if c := req.URL.Query().Get("category"); c != "" {
		filtered := all[:0]
		for _, n := range all {
			if n.Category == c {
				filtered = append(filtered, n)
			}
		}
		all = filtered
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(all)))
	writeJSON(w, http.StatusOK, all[min(offset, len(all)):min(offset+limit, len(all))])
}

// handleEpubCover 返回 EPUB 封面图片；没有封面时 404，客户端改用生成式书封。
func (r *Router) handleEpubCover(w http.ResponseWriter, req *http.Request) {
	_, physical, ok := r.canonicalTextFor(w, req, req.URL.Query().Get("path"))
	if !ok {
		return
	}
	if !strings.EqualFold(path.Ext(physical), ".epub") {
		http.NotFound(w, req)
		return
	}
	book, err := text.InspectEpub(req.Context(), physical)
	if err != nil || book.Cover == "" {
		http.NotFound(w, req)
		return
	}
	data, err := text.ReadEpubEntryContext(req.Context(), physical, book.Cover)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// handleRecentText 返回当前账号最近阅读的书（最多 20 本），附书名与进度。
func (r *Router) handleRecentText(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	if user.ID <= 0 || r.userState == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	list, err := r.userState.RecentText(req.Context(), user.ID, 20)
	if err != nil {
		http.Error(w, "position storage unavailable", http.StatusServiceUnavailable)
		return
	}
	type recent struct {
		novel
		ChapterIndex int     `json:"chapter_index"`
		ScrollRatio  float64 `json:"scroll_ratio"`
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
		return
	}
	out := []recent{}
	for _, p := range list {
		if !access.allows(p.DocumentID) {
			continue
		}
		if _, physical, err := r.canonicalText(p.DocumentID); err != nil {
			continue // 所在库离线或文件已移走
		} else if _, err := os.Stat(physical); err != nil {
			continue
		}
		_, rel, _ := strings.Cut(p.DocumentID, ":")
		rel = strings.TrimPrefix(rel, novelCategory+"/")
		ext := path.Ext(rel)
		libID, _, _ := strings.Cut(p.DocumentID, ":")
		out = append(out, recent{novel{ID: p.DocumentID, Title: strings.TrimSuffix(path.Base(rel), ext), Category: novelCategoryOf(rel),
			Format: strings.TrimPrefix(strings.ToLower(ext), "."), LibraryID: libID}, p.ChapterIndex, p.ScrollRatio})
	}
	writeJSON(w, http.StatusOK, out)
}
