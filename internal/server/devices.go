package server

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"omnihub/internal/auth"
)

func clearDeviceCookie(w http.ResponseWriter, req *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: auth.DeviceCookie, Value: "", Path: "/", HttpOnly: true, Secure: req.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func (r *Router) handleDevices(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	if user.Role != auth.RoleAdmin || user.ID <= 0 {
		http.Error(w, "administrator required", 403)
		return
	}
	switch req.Method {
	case http.MethodGet:
		devices, err := r.authDB.ListDevices(req.Context())
		if err != nil {
			http.Error(w, "device store unavailable", 503)
			return
		}
		writeJSON(w, 200, map[string]any{"devices": devices, "current_device_id": auth.DeviceID(req.Context()), "can_enroll": auth.IsDirectLAN(req) && auth.AuthenticationMethod(req.Context()) == "session", "lan": auth.IsDirectLAN(req), "capacity": auth.MaxTrustedDevices, "lifetime_days": 180})
	case http.MethodPost:
		if !auth.IsDirectLAN(req) || auth.AuthenticationMethod(req.Context()) != "session" {
			http.Error(w, "register the current device using a password login over a direct RFC1918 LAN connection", 403)
			return
		}
		var input struct {
			Name string `json:"name"`
		}
		decoder := json.NewDecoder(req.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "invalid device registration", 400)
			return
		}
		ip, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			ip = req.RemoteAddr
		}
		device, token, err := r.authDB.RegisterDevice(req.Context(), user.ID, auth.CredentialHash(req.Context()), input.Name, ip)
		if err != nil {
			status := 503
			if errors.Is(err, auth.ErrInvalidDevice) {
				status = 400
			} else if errors.Is(err, auth.ErrDeviceSession) {
				status = 403
			} else if errors.Is(err, auth.ErrDeviceCapacity) {
				status = 409
			}
			http.Error(w, "device registration failed: "+err.Error(), status)
			return
		}
		r.webGames.revokeCredential(auth.CredentialHash(req.Context()))
		http.SetCookie(w, &http.Cookie{Name: auth.DeviceCookie, Value: token, Path: "/", HttpOnly: true, Secure: req.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(auth.DeviceLifetime.Seconds()), Expires: device.ExpiresAt})
		http.SetCookie(w, &http.Cookie{Name: "omni_token", Value: "", Path: "/", HttpOnly: true, Secure: req.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		writeJSON(w, 200, map[string]any{"device": device})
	case http.MethodDelete:
		id := req.URL.Query().Get("id")
		if err := r.authDB.RevokeDevice(req.Context(), id); err != nil {
			http.Error(w, "device revocation failed: "+err.Error(), 400)
			return
		}
		r.webGames.revokeDevice(id)
		if id == auth.DeviceID(req.Context()) {
			clearDeviceCookie(w, req)
		}
		writeJSON(w, 200, map[string]string{"status": "revoked"})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (r *Router) handleLogout(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	deviceHash := ""
	if cookie, err := req.Cookie(auth.DeviceCookie); err == nil && len(cookie.Value) == 64 {
		deviceHash = auth.HashToken(cookie.Value)
	}
	if err := r.authDB.RevokeCredential(req.Context(), auth.SessionHash(req), deviceHash); err != nil {
		http.Error(w, "logout store unavailable", 503)
		return
	}
	r.webGames.revokeCredential(auth.SessionHash(req))
	r.webGames.revokeCredential(deviceHash)
	clearDeviceCookie(w, req)
	http.SetCookie(w, &http.Cookie{Name: "omni_token", Value: "", Path: "/", HttpOnly: true, Secure: req.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, 200, map[string]string{"status": "logged_out"})
}
