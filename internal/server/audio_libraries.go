package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"omnihub/internal/audio"
)

func (r *Router) listAudioLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	albums := make([]*audio.Album, 0, limit+1)
	for _, lib := range libraries {
		root, err := r.libMgr.ResolveCategoryPath(lib.ID+":media_library/audio", "media_library/audio")
		if err != nil {
			http.Error(w, "audio storage boundary invalid", 503)
			return
		}
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			http.Error(w, "audio storage unavailable", 503)
			return
		}
		dirs, total, err := r.audioCatalog.DirectoryPage(req.Context(), root, limit+1-len(albums), offset)
		if err != nil {
			http.Error(w, "audio catalog unavailable or exceeds capacity", 503)
			return
		}
		if offset >= total {
			offset -= total
			continue
		}
		offset = 0
		for _, dir := range dirs {
			logical := lib.ID + ":" + filepath.ToSlash(filepath.Join("media_library/audio", dir.Path))
			albums = append(albums, &audio.Album{ID: logical, Title: filepath.Base(filepath.Join(root, dir.Path)), TrackCount: dir.Count, Tracks: []*audio.Track{}, LibraryID: lib.ID, LibraryName: lib.Name})
		}
		if len(albums) > limit {
			break
		}
	}
	more := len(albums) > limit
	if more {
		albums = albums[:limit]
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(more))
	writeJSON(w, 200, albums)
}

func (r *Router) handleAudioCover(w http.ResponseWriter, req *http.Request) {
	path, err := r.libMgr.ResolveCategoryPath(req.URL.Query().Get("path"), "media_library/audio")
	if err != nil {
		http.Error(w, "cover not found", 404)
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	if audio.SupportedExtensions[ext] {
		metadata, err := r.readAudioMetadata(req, true)
		if err != nil {
			http.Error(w, "embedded cover unavailable", 422)
			return
		}
		if !metadata.HasCover {
			http.Error(w, "embedded cover not found", 404)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(metadata.Picture))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = w.Write(metadata.Picture)
		return
	}
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		http.Error(w, "unsupported cover format", 400)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "cover file unavailable", 404)
		return
	}
	http.ServeFile(w, req, path)
}
