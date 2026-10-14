package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"omnihub/internal/auth"
	"omnihub/internal/library"
	"path/filepath"
	"testing"
	"time"
)

func TestGameSessionsRequireRealNativeCredential(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	owner, err := db.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(owner.ID, "web-owner", time.Hour); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(library.NewManager(), db, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.AddCookie(&http.Cookie{Name: "omni_token", Value: "web-owner"})
		req.Header.Set("X-Omni-Client", "gui")
		if token != "" {
			req.Header.Set("X-Omni-Local-Token", token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if w := request("GET", "/api/games/session", "", ""); w.Code != 403 {
		t.Fatalf("web admin became native: %d", w.Code)
	}
	status := request("GET", "/api/auth/status", "", "")
	var session struct {
		User struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &session); err != nil || session.User.Role != auth.RoleAdmin {
		t.Fatal("web permission test lacked a real authenticated administrator")
	}
	if w := request("GET", "/api/games/session", "", "native-token"); w.Code != 200 || w.Body.String() != "null\n" {
		t.Fatalf("idle status: %d %s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/games/stop", `{"id":"missing"}`, "native-token"); w.Code != 404 {
		t.Fatal("unknown identity accepted")
	}
	if w := request("POST", "/api/games/stop", `{"id":"missing","pid":1}`, "native-token"); w.Code != 400 {
		t.Fatal("raw PID input accepted")
	}
}
