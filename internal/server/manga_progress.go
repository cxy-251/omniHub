package server

import (
	"encoding/json"
	"errors"
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
	if req.Method == http.MethodPost {
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
		positionStored.MangaID = canonical
	}
	writeJSON(w, 200, positionStored)
}
