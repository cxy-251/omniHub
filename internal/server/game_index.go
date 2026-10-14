package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/scanner"
	"strings"
)

// Index publication is an explicit host operation. Browsing never mutates a
// library; scans exclude task/game startup and state migration while active.
func (r *Router) handleGameIndex(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !r.gameControlAllowed(w, req) {
		return
	}
	if auth.UserFromContext(req.Context()).Role != auth.RoleAdmin {
		http.Error(w, "administrator required", 403)
		return
	}
	var input struct {
		LibraryID string `json:"library_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.LibraryID == "" || len(input.LibraryID) > 128 {
		http.Error(w, "invalid library identity", 400)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "unexpected request data", 400)
		return
	}
	if !r.runtimeMu.TryLock() {
		http.Error(w, "host work is busy", 409)
		return
	}
	defer r.runtimeMu.Unlock()
	if r.taskMgr.HasRunningTasks() || r.gameRuns.HasRunning() {
		http.Error(w, "stop active tasks and games before indexing", 409)
		return
	}
	var selected *library.Library
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.ID == input.LibraryID {
			selected = lib
			break
		}
	}
	if selected == nil {
		http.Error(w, "library not found", 404)
		return
	}
	if selected.ReadOnly {
		http.Error(w, "cannot write a read-only game index", 403)
		return
	}
	directory, err := r.gameStorageDirectory(selected.RootPath)
	if err != nil {
		http.Error(w, "game storage unavailable", 503)
		return
	}
	categories := make([]string, 0)
	for _, dir := range r.libMgr.ActiveDirectories() {
		if strings.HasPrefix(dir, "standalone_games/") && strings.HasSuffix(dir, "_games") {
			categories = append(categories, strings.TrimSuffix(strings.TrimPrefix(dir, "standalone_games/"), "_games"))
		}
	}
	count, err := scanner.ScanGameCategories(req.Context(), directory, categories)
	if err != nil {
		http.Error(w, "game scan failed: "+err.Error(), 422)
		return
	}
	writeJSON(w, 200, map[string]any{"library_id": selected.ID, "indexed": count})
}
