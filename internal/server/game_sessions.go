package server

import (
	"encoding/json"
	"net/http"
	"omnihub/internal/auth"
)

func (r *Router) gameControlAllowed(w http.ResponseWriter, req *http.Request) bool {
	if !auth.IsTrustedLocalFromContext(req.Context()) || auth.ClientFromContext(req.Context()) != auth.TargetGUI {
		http.Error(w, "trusted native client required", 403)
		return false
	}
	return true
}

func (r *Router) handleGameSession(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !r.gameControlAllowed(w, req) {
		return
	}
	id := req.URL.Query().Get("id")
	if id == "" {
		active := r.gameRuns.Active()
		user := auth.UserFromContext(req.Context())
		if active != nil && active.UserID != user.ID && user.Role != auth.RoleAdmin {
			http.Error(w, "session belongs to another account", 403)
			return
		}
		writeJSON(w, 200, active)
		return
	}
	if len(id) > 128 {
		http.Error(w, "invalid session identity", 400)
		return
	}
	status, err := r.gameRuns.Snapshot(id)
	if err != nil {
		http.Error(w, "session not found", 404)
		return
	}
	user := auth.UserFromContext(req.Context())
	if status.UserID != user.ID && user.Role != auth.RoleAdmin {
		http.Error(w, "session belongs to another account", 403)
		return
	}
	writeJSON(w, 200, status)
}

func (r *Router) handleGameStop(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !r.gameControlAllowed(w, req) {
		return
	}
	var payload struct {
		ID string `json:"id"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || payload.ID == "" || len(payload.ID) > 128 {
		http.Error(w, "invalid session identity", 400)
		return
	}
	status, err := r.gameRuns.Snapshot(payload.ID)
	if err != nil {
		http.Error(w, "session not found", 404)
		return
	}
	user := auth.UserFromContext(req.Context())
	if status.UserID != user.ID && user.Role != auth.RoleAdmin {
		http.Error(w, "session belongs to another account", 403)
		return
	}
	if err := r.gameRuns.Stop(payload.ID); err != nil {
		http.Error(w, "stop request failed", 503)
		return
	}
	writeJSON(w, 202, map[string]string{"status": "stopping"})
}
