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
	"omnihub/internal/text"
	"omnihub/internal/userstate"
)

func (r *Router) handleTextProgress(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var position userstate.TextPosition
	position.DocumentID = req.URL.Query().Get("document_id")
	if req.Method == http.MethodPost {
		if err := json.NewDecoder(req.Body).Decode(&position); err != nil {
			http.Error(w, "invalid reading position", 400)
			return
		}
	}
	logical, physical, ok := r.canonicalTextFor(w, req, position.DocumentID)
	if !ok {
		return
	}
	ext := strings.ToLower(filepath.Ext(physical))
	if !text.SupportedExtensions[ext] {
		http.Error(w, "unsupported document", 400)
		return
	}
	info, err := os.Lstat(physical)
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "document unavailable", 404)
		return
	}
	user := auth.UserFromContext(req.Context())
	if user.ID <= 0 {
		if req.Method == http.MethodGet {
			writeJSON(w, 200, map[string]any{"position": nil, "can_save": false})
		} else {
			http.Error(w, "logged-in account required", 403)
		}
		return
	}
	if r.userState == nil {
		http.Error(w, "user state unavailable", 503)
		return
	}
	version := fmt.Sprintf("%x-%x", info.Size(), info.ModTime().UnixNano())
	if req.Method == http.MethodPost {
		// A response from an earlier file revision must not save a misleading cursor.
		if position.FileVersion != "" && position.FileVersion != version {
			http.Error(w, "document changed; reopen before saving position", 409)
			return
		}
		position.UserID = user.ID
		position.DocumentID = logical
		position.FileVersion = version
		if position.ByteOffset > info.Size() {
			http.Error(w, "position beyond document size", 400)
			return
		}
		if err := r.userState.SaveText(req.Context(), &position); err != nil {
			if errors.Is(err, userstate.ErrInvalidProgress) {
				http.Error(w, "invalid reading position", 400)
			} else {
				http.Error(w, "position storage unavailable", 503)
			}
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	p, err := r.userState.Text(req.Context(), user.ID, logical)
	if err != nil {
		http.Error(w, "position storage unavailable", 503)
		return
	}
	if p != nil && (p.FileVersion != version || p.ByteOffset > info.Size()) {
		p = nil
	}
	writeJSON(w, 200, map[string]any{"position": p, "can_save": true, "file_version": version, "account_id": user.ID})
}
