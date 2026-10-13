package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/audio"
)

func (r *Router) readAudioMetadata(req *http.Request) (metadata audio.Metadata, result error) {
	path, err := r.libMgr.ResolveCategoryPath(req.URL.Query().Get("path"), "media_library/audio")
	if err != nil {
		return metadata, err
	}
	if !audio.SupportedExtensions[strings.ToLower(filepath.Ext(path))] {
		return metadata, errors.New("unsupported audio format")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return metadata, err
	}
	if !info.Mode().IsRegular() {
		return metadata, errors.New("audio is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return metadata, err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	opened, err := file.Stat()
	if err != nil {
		return metadata, err
	}
	if !os.SameFile(info, opened) {
		return metadata, errors.New("audio changed while opening")
	}
	return audio.ReadMetadata(req.Context(), file)
}

func (r *Router) handleAudioMetadata(w http.ResponseWriter, req *http.Request) {
	metadata, err := r.readAudioMetadata(req)
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
