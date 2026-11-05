package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omnihub/internal/auth"
)

func TestSC2WindowControlRequiresTrustedNativeClient(t *testing.T) {
	db, err := auth.OpenDB(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateUser("admin", "password", auth.RoleAdmin, auth.LevelAdmin); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(1, "web-session", time.Hour); err != nil {
		t.Fatal(err)
	}
	router := &Router{authDB: db}
	handler := auth.NewMiddleware(db, "local-secret").Handler(http.HandlerFunc(router.handleSC2Control))
	for _, tc := range []struct {
		name, action string
		local        bool
		want         int
	}{
		{"logged-in browser cannot close native window", "close", false, http.StatusForbidden},
		{"local client can close absent window", "close", true, http.StatusOK},
		{"local client cannot submit arbitrary commands", "shell", true, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/sc2/control", strings.NewReader(`{"action":"`+tc.action+`"}`))
			req.RemoteAddr = "127.0.0.1:18000"
			if tc.local {
				req.Header.Set("X-Omni-Local-Token", "local-secret")
			} else {
				req.AddCookie(&http.Cookie{Name: "omni_token", Value: "web-session"})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}
