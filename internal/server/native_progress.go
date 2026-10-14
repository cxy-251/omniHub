package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"omnihub/internal/auth"
	"omnihub/internal/userstate"
)

// Native progress accepts opaque digests, never arbitrary paths for the server
// to inspect. The actual authenticated host account owns all reads and writes.
func (r *Router) handleNativeProgress(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !auth.IsTrustedLocalFromContext(req.Context()) || auth.ClientFromContext(req.Context()) != auth.TargetGUI {
		http.Error(w, "trusted native client required", 403)
		return
	}
	user := auth.UserFromContext(req.Context())
	if user == nil || user.ID <= 0 {
		http.Error(w, "initialized account required", 403)
		return
	}
	if req.Method == http.MethodGet {
		position, err := r.userState.Native(req.Context(), user.ID, req.URL.Query().Get("id"), req.URL.Query().Get("version"))
		if err != nil {
			if errors.Is(err, userstate.ErrInvalidProgress) {
				http.Error(w, "invalid media identity", 400)
			} else {
				http.Error(w, "progress storage unavailable", 503)
			}
			return
		}
		writeJSON(w, 200, map[string]any{"account_id": user.ID, "position": position})
		return
	}
	var input struct {
		userstate.NativePosition
		AccountID int64 `json:"account_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "invalid progress", 400)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "unexpected trailing data", 400)
		return
	}
	if input.AccountID != user.ID {
		http.Error(w, "account changed; reload progress", 409)
		return
	}
	if err := r.userState.SaveNative(req.Context(), user.ID, input.NativePosition); err != nil {
		if errors.Is(err, userstate.ErrInvalidProgress) {
			http.Error(w, "invalid progress", 400)
		} else {
			http.Error(w, "progress storage unavailable", 503)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"account_id": user.ID, "status": "ok"})
}
