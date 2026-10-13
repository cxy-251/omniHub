package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/manga"
)

func releaseMangaReader(lease *manga.ReaderLease) {
	if err := lease.Close(); err != nil {
		log.Printf("release CBZ reader: %v", err)
	}
}

func (r *Router) indexedMangaCover(req *http.Request, logical string, lib *library.Library) (string, string, error) {
	dir, err := r.moduleDirectory(lib.RootPath, "media_library/manga", "manga.db")
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(filepath.Join(dir, "manga.db")); errors.Is(err, os.ErrNotExist) {
		return "", "", nil
	} else if err != nil {
		return "", "", err
	}
	db, err := r.getMangaDBMode(lib.RootPath, true)
	if err != nil {
		return "", "", err
	}
	_, relative, ok := strings.Cut(logical, ":media_library/manga/")
	if !ok {
		return "", "", errors.New("invalid book identity")
	}
	return db.IndexedCover(req.Context(), logical, relative)
}

func (r *Router) handleMangaCustomCover(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if auth.UserFromContext(req.Context()).ID <= 0 {
		http.Error(w, "logged-in account required", 403)
		return
	}
	var payload struct {
		Path  string `json:"path"`
		Entry string `json:"entry"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || len(payload.Entry) > 4096 {
		http.Error(w, "invalid cover selection", 400)
		return
	}
	logical, lib, err := r.mangaIdentity(payload.Path)
	if err != nil {
		http.Error(w, "book unavailable", 404)
		return
	}
	if lib.ReadOnly {
		http.Error(w, "book index is read-only", 403)
		return
	}
	id, _, err := r.indexedMangaCover(req, logical, lib)
	if err != nil {
		http.Error(w, "book index unavailable", 503)
		return
	}
	if id == "" {
		http.Error(w, "book is not indexed", 404)
		return
	}
	if payload.Entry != "" {
		physical, err := r.libMgr.ResolveCategoryPath(logical, "media_library/manga")
		if err != nil {
			http.Error(w, "book unavailable", 404)
			return
		}
		lease, err := r.mangaReaders.Acquire(req.Context(), physical)
		if err != nil {
			http.Error(w, "book archive unavailable", 503)
			return
		}
		valid := lease.Reader.GetCoverEntry(payload.Entry) == payload.Entry && manga.ImageExtensions[strings.ToLower(filepath.Ext(payload.Entry))]
		if err := lease.Close(); err != nil {
			http.Error(w, "book archive unavailable", 503)
			return
		}
		if !valid {
			http.Error(w, "cover must be an existing image page", 400)
			return
		}
	}
	db, err := r.getMangaDB(lib.RootPath)
	if err != nil {
		http.Error(w, "book index unavailable", 503)
		return
	}
	if err := db.SetCustomCoverContext(req.Context(), id, payload.Entry); err != nil {
		http.Error(w, "cover update failed", 503)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
