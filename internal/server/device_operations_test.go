package server

import (
	"net/http"
	"net/http/httptest"
	"omnihub/internal/auth"
	"path/filepath"
	"testing"
	"time"
)

func TestDeviceOperationsRequireNativeHost(t *testing.T) {
	db, err := auth.OpenDB(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.CreateUser("admin", "password", auth.RoleAdmin, auth.LevelAdmin); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateSession(1, "web-session", time.Hour); err != nil {
		t.Fatal(err)
	}
	router := &Router{deviceOps: newDeviceOperations(t.TempDir())}
	handler := auth.NewMiddleware(db, "local-secret").Handler(http.HandlerFunc(router.handleDeviceOperations))
	for _, tc := range []struct {
		name, addr, token string
		forward           bool
		want              int
	}{
		{"native", "127.0.0.1:18000", "local-secret", false, 200},
		{"browser", "127.0.0.1:18000", "", false, 403},
		{"LAN", "192.168.1.2:18000", "local-secret", false, 403},
		{"proxy", "127.0.0.1:18000", "local-secret", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/devices/status", nil)
			req.RemoteAddr = tc.addr
			req.Header.Set("X-Omni-Local-Token", tc.token)
			req.AddCookie(&http.Cookie{Name: "omni_token", Value: "web-session"})
			if tc.forward {
				req.Header.Set("X-Forwarded-For", "192.168.1.2")
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.want == 403 {
				for _, action := range []string{"deploy", "manga/plan", "manga/start", "pause"} {
					req.Method = "POST"
					req.URL.Path = "/api/devices/" + action
					req.Body = http.NoBody
					w = httptest.NewRecorder()
					handler.ServeHTTP(w, req)
					if w.Code != 403 {
						t.Fatalf("mutation accepted: %s %d", action, w.Code)
					}
				}
			}
		})
	}
}
