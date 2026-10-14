package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode"

	"omnihub/internal/auth"
)

func accountError(w http.ResponseWriter, err error) {
	status := 503
	if errors.Is(err, auth.ErrAccountInput) {
		status = 400
	} else if errors.Is(err, auth.ErrAccountMissing) {
		status = 404
	} else if errors.Is(err, auth.ErrAccountExists) || errors.Is(err, auth.ErrLastAdmin) || errors.Is(err, auth.ErrCredentialChanged) {
		status = 409
	}
	if status == 503 {
		http.Error(w, "account storage unavailable", status)
	} else {
		http.Error(w, err.Error(), status)
	}
}

func (r *Router) handleAccountChange(w http.ResponseWriter, req *http.Request) {
	if auth.UserFromContext(req.Context()).Role != auth.RoleAdmin {
		http.Error(w, "administrator required", 403)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var input struct {
		UserID   int64  `json:"user_id"`
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Level    int    `json:"level"`
		Status   string `json:"status"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid account request", 400)
		return
	}
	switch req.URL.Path {
	case "/api/admin/users/create":
		name := strings.TrimSpace(input.Username)
		if name == "" || len(name) > 64 || strings.ContainsFunc(name, unicode.IsControl) || input.Password == "" || len(input.Password) > 72 || (input.Role != auth.RoleUser && input.Role != auth.RoleAdmin) || (input.Role == auth.RoleAdmin && input.Level != auth.LevelAdmin) || (input.Role == auth.RoleUser && (input.Level < 0 || input.Level > 4)) {
			http.Error(w, "invalid account fields", 400)
			return
		}
		// CreateUser performs the final unique-name check in SQLite.
		user, err := r.authDB.CreateUser(name, input.Password, input.Role, input.Level)
		if err != nil {
			accountError(w, err)
			return
		}
		writeJSON(w, 201, user)
	case "/api/admin/users/status":
		if err := r.authDB.SetUserStatus(req.Context(), input.UserID, input.Status); err != nil {
			accountError(w, err)
			return
		}
		r.webGames.revokeUser(input.UserID)
		writeJSON(w, 200, map[string]string{"status": "updated"})
	case "/api/admin/users/password":
		if err := r.authDB.SetPassword(req.Context(), input.UserID, input.Password); err != nil {
			accountError(w, err)
			return
		}
		r.webGames.revokeUser(input.UserID)
		writeJSON(w, 200, map[string]string{"status": "password_changed"})
	default:
		http.NotFound(w, req)
	}
}

func (r *Router) handleOwnPassword(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	current := auth.UserFromContext(req.Context())
	if current.ID <= 0 {
		http.Error(w, "account login required", 401)
		return
	}
	var input struct {
		Current  string `json:"current_password"`
		Password string `json:"new_password"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Current == "" || len(input.Current) > 72 || input.Password == "" || len(input.Password) > 72 {
		http.Error(w, "invalid password change", 400)
		return
	}
	verified, err := r.authDB.Authenticate(req.RemoteAddr, current.Username, input.Current)
	if err != nil {
		http.Error(w, "current password verification failed", 401)
		return
	}
	if verified.ID != current.ID {
		http.Error(w, "account changed; sign in again", 409)
		return
	}
	if err = r.authDB.SetVerifiedPassword(req.Context(), verified, input.Password); err != nil {
		accountError(w, err)
		return
	}
	r.webGames.revokeUser(current.ID)
	clearDeviceCookie(w, req)
	http.SetCookie(w, &http.Cookie{Name: "omni_token", Value: "", Path: "/", HttpOnly: true, Secure: req.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, 200, map[string]string{"status": "password_changed", "sign_in_required": "true"})
}
