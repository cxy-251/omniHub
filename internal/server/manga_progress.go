package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/manga"
	"omnihub/internal/userstate"
)

func (r *Router) mangaIdentity(id string) (string, *library.Library, error) {
	if id == "" || len(id) > 4096 {
		return "", nil, errors.New("invalid book identity")
	}
	lib := r.libMgr.GetPrimary()
	if prefix, _, ok := strings.Cut(id, ":"); ok {
		lib = nil
		for _, candidate := range r.libMgr.ListLibraries() {
			if candidate.ID == prefix {
				lib = candidate
				break
			}
		}
	}
	if lib == nil || !lib.IsOnline {
		return "", nil, errors.New("book library unavailable")
	}
	path, err := r.libMgr.ResolveCategoryPath(id, "media_library/manga")
	if err != nil {
		return "", nil, err
	}
	if strings.ToLower(filepath.Ext(path)) != ".cbz" {
		return "", nil, errors.New("book must be CBZ")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("book must be a regular file")
	}
	relative, err := filepath.Rel(lib.RootPath, path)
	if err != nil {
		return "", nil, err
	}
	return lib.ID + ":" + filepath.ToSlash(relative), lib, nil
}

func (r *Router) handleMangaProgress(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var position manga.MangaProgress
	position.MangaID = req.URL.Query().Get("manga_id")
	if req.Method == http.MethodPost {
		if err := json.NewDecoder(req.Body).Decode(&position); err != nil {
			http.Error(w, "invalid reading progress", 400)
			return
		}
	}
	provided := position.MangaID
	canonical, lib, err := r.mangaIdentity(provided)
	if err != nil {
		http.Error(w, "book unavailable", 404)
		return
	}
	user := auth.UserFromContext(req.Context())
	if user.ID <= 0 {
		if req.Method == http.MethodGet {
			writeJSON(w, 200, nil)
		} else {
			http.Error(w, "logged-in account required", 403)
		}
		return
	}
	if r.userState == nil {
		http.Error(w, "user state unavailable", 503)
		return
	}
	physical, err := r.libMgr.ResolveCategoryPath(canonical, "media_library/manga")
	if err != nil {
		http.Error(w, "book unavailable", 404)
		return
	}
	info, err := os.Stat(physical)
	if err != nil {
		http.Error(w, "book unavailable", 404)
		return
	}
	version := fmt.Sprintf("%x-%x", info.Size(), info.ModTime().UnixNano())
	if req.Method == http.MethodPost {
		if position.FileVersion != "" && position.FileVersion != version {
			http.Error(w, "book changed; reopen before saving", 409)
			return
		}
		if position.PositionMode == "page" {
			position.FileVersion = version
		}
		position.MangaID = canonical
		position.UserID = user.ID
		if err := r.userState.SaveManga(req.Context(), &position); err != nil {
			if errors.Is(err, userstate.ErrInvalidProgress) {
				http.Error(w, "invalid reading progress", 400)
			} else {
				http.Error(w, "progress storage unavailable", 503)
			}
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	positionStored, err := r.userState.Manga(req.Context(), user.ID, canonical)
	if err != nil {
		http.Error(w, "progress storage unavailable", 503)
		return
	}
	if positionStored == nil {
		// Compatibility only: old module progress is read without creating an index
		// or copying unrelated account rows into the global database.
		dir, err := r.moduleDirectory(lib.RootPath, "media_library/manga", "manga.db")
		if err != nil {
			http.Error(w, "legacy progress boundary invalid", 503)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "manga.db")); err == nil {
			db, err := r.getMangaDBMode(lib.RootPath, true)
			if err != nil {
				http.Error(w, "legacy progress unavailable", 503)
				return
			}
			_, relative, _ := strings.Cut(canonical, ":media_library/manga/")
			positionStored, err = db.LegacyProgressForPath(req.Context(), user.ID, canonical, provided, relative)
			if err != nil {
				http.Error(w, "legacy progress unavailable", 503)
				return
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "legacy progress unavailable", 503)
			return
		}
	}
	if positionStored != nil {
		if positionStored.FileVersion != "" && positionStored.FileVersion != version {
			writeJSON(w, 200, nil)
			return
		}
		positionStored.MangaID = canonical
		if positionStored.PositionMode == "" {
			positionStored.PositionMode = "book"
		}
	}
	writeJSON(w, 200, positionStored)
}

// handleMangaRecent 返回当前账号最近读过的漫画（最多 12 本），附读到的页；文件已不在的跳过。
func (r *Router) handleMangaRecent(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "manga", auth.TargetAll) {
		return
	}
	type recent struct {
		Title       string `json:"title"`
		LogicalPath string `json:"logical_path"`
		PageIndex   int    `json:"page_index"`
	}
	out := []recent{}
	user := auth.UserFromContext(req.Context())
	if user.ID <= 0 || r.userState == nil {
		writeJSON(w, 200, out)
		return
	}
	list, err := r.userState.RecentManga(req.Context(), user.ID, 12)
	if err != nil {
		http.Error(w, "progress storage unavailable", 503)
		return
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", 503)
		return
	}
	for _, p := range list {
		if !access.allows(p.MangaID) {
			continue
		}
		if _, _, err := r.mangaIdentity(p.MangaID); err != nil {
			continue // 所在库离线或文件已移走
		}
		name := p.MangaID[strings.LastIndex(p.MangaID, "/")+1:]
		out = append(out, recent{Title: strings.TrimSuffix(name, filepath.Ext(name)), LogicalPath: p.MangaID, PageIndex: p.PageIndex})
	}
	writeJSON(w, 200, out)
}

// handleMangaLikes：GET 返回当前账号点赞的漫画（逻辑路径）；POST {"path": "...", "liked": true} 点赞或取消。
// 与短视频、相册共用按账号的点赞表（domain = manga）。
func (r *Router) handleMangaLikes(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "manga", auth.TargetAll) {
		return
	}
	user := auth.UserFromContext(req.Context())
	if r.userState == nil {
		http.Error(w, "user state unavailable", 503)
		return
	}
	switch req.Method {
	case http.MethodGet:
		if user.ID <= 0 {
			writeJSON(w, 200, []string{})
			return
		}
		likes, err := r.userState.Likes(req.Context(), user.ID, "manga")
		if err != nil {
			http.Error(w, "like storage unavailable", 503)
			return
		}
		if likes == nil {
			likes = []string{}
		}
		writeJSON(w, 200, likes)
	case http.MethodPost:
		if user.ID <= 0 {
			http.Error(w, "login required", http.StatusForbidden)
			return
		}
		var in struct {
			Path  string `json:"path"`
			Liked bool   `json:"liked"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 8<<10)).Decode(&in); err != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		if _, _, err := r.mangaIdentity(in.Path); err != nil {
			http.Error(w, "book unavailable", 404)
			return
		}
		if !r.allowPath(w, req, in.Path) {
			return
		}
		// 点赞按书架条目用的逻辑路径（<库ID>:media_library/manga/…）记。
		if err := r.userState.SetLike(req.Context(), user.ID, "manga", in.Path, in.Liked); err != nil {
			http.Error(w, "like storage unavailable", 503)
			return
		}
		writeJSON(w, 200, map[string]bool{"liked": in.Liked})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
