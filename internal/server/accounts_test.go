package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/library"
)

func TestAccountAdministrationAndOwnPassword(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner, err := db.CreateInitialAdmin("owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(library.NewManager(), db, state, "local")
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	call := func(method, path, body, token string, local bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "192.168.1.2:1000"
		if local {
			req.RemoteAddr = "127.0.0.1:1000"
			req.Header.Set("X-Omni-Local-Token", "local")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	check := func(code int, method, path, body, token string, local bool) *httptest.ResponseRecorder {
		t.Helper()
		r := call(method, path, body, token, local)
		if r.Code != code {
			t.Fatalf("%s %s: %d %s", method, path, r.Code, r.Body.String())
		}
		return r
	}
	body := `{"username":"reader <script>","password":"original","role":"user","level":1}`
	check(403, "POST", "/api/admin/users/create", body, "", false)
	check(400, "POST", "/api/admin/users/create", `{"username":"bad","unknown":true}`, "", true)
	created := check(201, "POST", "/api/admin/users/create", body, "", true)
	if strings.Contains(created.Body.String(), "hash") || strings.Contains(created.Body.String(), "original") {
		t.Fatal("credential leaked")
	}
	var reader auth.User
	if err = json.Unmarshal(created.Body.Bytes(), &reader); err != nil {
		t.Fatal(err)
	}
	check(409, "POST", "/api/admin/users/create", body, "", true)
	page := check(200, "GET", "/api/admin/users?limit=1&offset=0", "", "", true)
	if page.Header().Get("X-Has-More") != "true" {
		t.Fatal("missing next page")
	}
	check(409, "POST", "/api/admin/users/status", fmt.Sprintf(`{"user_id":%d,"status":"disabled"}`, owner.ID), "", true)
	if err = db.CreateSession(reader.ID, "reader", time.Hour); err != nil {
		t.Fatal(err)
	}
	check(403, "POST", "/api/admin/users/password", fmt.Sprintf(`{"user_id":%d,"password":"attack"}`, owner.ID), "reader", false)
	check(401, "POST", "/api/auth/password", `{"current_password":"wrong","new_password":"next"}`, "reader", false)
	check(200, "POST", "/api/auth/password", `{"current_password":"original","new_password":"next"}`, "reader", false)
	if u, err := db.GetUserByToken("reader"); err != nil || u != nil {
		t.Fatal("own change left session", err)
	}
	if _, err = db.Authenticate("new", "reader <script>", "next"); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateSession(reader.ID, "reader2", time.Hour); err != nil {
		t.Fatal(err)
	}
	check(200, "POST", "/api/admin/users/password", fmt.Sprintf(`{"user_id":%d,"password":"reset"}`, reader.ID), "", true)
	if u, err := db.GetUserByToken("reader2"); err != nil || u != nil {
		t.Fatal("reset left session", err)
	}
	if _, err = db.Authenticate("reset", "reader <script>", "reset"); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateSession(reader.ID, "reader3", time.Hour); err != nil {
		t.Fatal(err)
	}
	check(200, "POST", "/api/admin/users/status", fmt.Sprintf(`{"user_id":%d,"status":"disabled"}`, reader.ID), "", true)
	if u, err := db.GetUserByToken("reader3"); err != nil || u != nil {
		t.Fatal("disable left session", err)
	}
	check(401, "POST", "/api/auth/login", `{"username":"reader <script>","password":"reset"}`, "", false)
	check(200, "POST", "/api/admin/users/status", fmt.Sprintf(`{"user_id":%d,"status":"active"}`, reader.ID), "", true)
	check(200, "POST", "/api/auth/login", `{"username":"reader <script>","password":"reset"}`, "", false)
}
