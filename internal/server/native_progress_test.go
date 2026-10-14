package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"omnihub/internal/auth"
	"omnihub/internal/library"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeProgressRequiresHostAndAccountGuard(t *testing.T) {
	state := t.TempDir()
	users, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer users.Close()
	owner, err := users.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(library.NewManager(), users, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	id, version := strings.Repeat("a", 64), strings.Repeat("b", 64)
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", token)
		req.Header.Set("X-Omni-Client", "gui")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	path := "/api/native/progress?id=" + id + "&version=" + version
	if w := request("GET", path, "", ""); w.Code != 403 {
		t.Fatal("spoofed GUI read resume", w.Code)
	}
	w := request("GET", path, "", "native-token")
	var response struct {
		AccountID int64 `json:"account_id"`
		Position  any   `json:"position"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.AccountID != owner.ID || response.Position != nil {
		t.Fatal("empty native progress unavailable", w.Code, w.Body.String())
	}
	body := map[string]any{"id": id, "version": version, "position": 12.5, "duration": 90, "paused": true, "volume": 20, "rate": 1.5, "account_id": owner.ID}
	post := func() *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return request("POST", "/api/native/progress", string(data), "native-token")
	}
	if w := post(); w.Code != 200 {
		t.Fatal("valid native resume failed", w.Code, w.Body.String())
	}
	body["account_id"] = owner.ID + 1
	if w := post(); w.Code != 409 {
		t.Fatal("late account switch not rejected", w.Code)
	}
	body["account_id"] = owner.ID
	body["user_id"] = owner.ID + 1
	if w := post(); w.Code != 400 {
		t.Fatal("user injection accepted", w.Code)
	}
	if w := request("POST", "/api/native/progress", "{} {}", "native-token"); w.Code != 400 {
		t.Fatal("trailing JSON accepted", w.Code)
	}
	if w := request("GET", "/api/native/progress?id=physical-path&version="+version, "", "native-token"); w.Code != 400 {
		t.Fatal("physical media identity accepted", w.Code)
	}
	w = request("GET", path, "", "native-token")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "12.5") {
		t.Fatal("account guard changed existing state", w.Code, w.Body.String())
	}
}
