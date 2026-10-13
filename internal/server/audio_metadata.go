package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/audio"
)

func (r *Router) readAudioMetadata(req *http.Request, picture bool) (metadata audio.Metadata, result error) {
	path, err := r.libMgr.ResolveCategoryPath(req.URL.Query().Get("path"), "media_library/audio")
	if err != nil {
		return metadata, err
	}
	if !audio.SupportedExtensions[strings.ToLower(filepath.Ext(path))] {
		return metadata, errors.New("unsupported audio format")
	}
	return r.audioMetadata.Get(req.Context(), path, picture)
}

func (r *Router) handleAudioMetadata(w http.ResponseWriter, req *http.Request) {
	metadata, err := r.readAudioMetadata(req, false)
	if err != nil {
		status := 422
		if errors.Is(err, audio.ErrMetadataCapacity) {
			status = 413
		}
		if errors.Is(err, os.ErrNotExist) {
			status = 404
		}
		http.Error(w, "audio metadata unavailable or invalid", status)
		return
	}
	writeJSON(w, 200, metadata)
}
