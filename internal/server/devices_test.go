package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/games"
	"omnihub/internal/library"
)

func TestDeviceEnrollmentGamesAndImmediateRevocation(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner, err := db.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateSession(owner.ID, "proof", time.Hour); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	lib, err := manager.AddLibrary(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(lib.RootPath, "standalone_games")
	folder := filepath.Join(root, "rpg_games/001 - Game")
	if err = os.MkdirAll(filepath.Join(folder, "www"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(folder, "www/index.html"), []byte("<h1>private game</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := games.OpenDB(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = index.UpsertGames([]*games.Game{{ID: "001", Name: "Game", Category: "rpg", Target: "web_only", RelDir: "rpg_games/001 - Game"}}); err != nil {
		t.Fatal(err)
	}
	if err = index.Close(); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(manager, db, state, "local")
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	call := func(method, path, body, addr string, cookie *http.Cookie, session bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request.RemoteAddr = addr
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if session {
			request.Header.Set("Authorization", "Bearer proof")
		}
		if addr == "127.0.0.1:1234" {
			request.Header.Set("X-Omni-Local-Token", "local")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if denied := call("POST", "/api/admin/devices", `{"name":"wrong scope"}`, "8.8.8.8:1234", nil, true); denied.Code != 403 {
		t.Fatal("WAN enrollment accepted")
	}
	response := call("POST", "/api/admin/devices", `{"name":"Tablet <script>"}`, "192.168.1.2:1234", nil, true)
	if response.Code != 200 {
		t.Fatalf("enroll %d %s", response.Code, response.Body.String())
	}
	var value struct {
		Device auth.TrustedDevice `json:"device"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	var credential *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == auth.DeviceCookie {
			credential = cookie
		}
	}
	if credential == nil || !credential.HttpOnly || credential.SameSite != http.SameSiteStrictMode || credential.MaxAge <= 86400 || len(credential.Value) != 64 {
		t.Fatal("long credential cookie policy incorrect")
	}
	if strings.Contains(response.Body.String(), credential.Value) || strings.Contains(response.Body.String(), auth.HashToken(credential.Value)) {
		t.Fatal("device secret exposed in JSON")
	}
	if denied := call("POST", "/api/admin/devices", `{"name":"chain"}`, "192.168.1.2:1234", credential, false); denied.Code != 403 {
		t.Fatal("remembered credential enrolled another credential")
	}
	body := `{"library_id":"` + lib.ID + `","id":"001","client_id":"device-tab"}`
	opened := call("POST", "/api/games/web/open", body, "192.168.1.2:1234", credential, false)
	var grant struct {
		URL string `json:"url"`
	}
	if opened.Code != 201 || json.Unmarshal(opened.Body.Bytes(), &grant) != nil {
		t.Fatalf("device game open %d %s", opened.Code, opened.Body.String())
	}
	if asset := call("GET", grant.URL, "", "192.168.1.2:1234", nil, false); asset.Code != 200 {
		t.Fatalf("device game asset %d %s", asset.Code, asset.Body.String())
	}
	if asset := call("GET", grant.URL, "", "8.8.8.8:1234", nil, false); asset.Code != 403 {
		t.Fatal("device grant escaped LAN scope")
	}
	revoked := call("DELETE", "/api/admin/devices?id="+value.Device.ID, "", "127.0.0.1:1234", nil, false)
	if revoked.Code != 200 {
		t.Fatal("revocation failed")
	}
	status := call("GET", "/api/auth/status", "", "192.168.1.2:1234", credential, true)
	if !strings.Contains(status.Body.String(), `"logged_in":false`) {
		t.Fatalf("revoked credential retained session fallback: %s", status.Body.String())
	}
	if asset := call("GET", grant.URL, "", "192.168.1.2:1234", nil, false); asset.Code != 403 {
		t.Fatal("revoked device game grant still valid")
	}
	login := call("POST", "/api/auth/login", `{"username":"owner","password":"test-password"}`, "192.168.1.2:1234", credential, false)
	if login.Code != 200 {
		t.Fatalf("password reauthentication failed %d %s", login.Code, login.Body.String())
	}
	cleared := false
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == auth.DeviceCookie && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("password login did not clear stale device cookie")
	}
	var passwordCookie *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == "omni_token" {
			passwordCookie = cookie
		}
	}
	enrolledAgain := call("POST", "/api/admin/devices", `{"name":"Logout test"}`, "192.168.1.2:1234", passwordCookie, false)
	if enrolledAgain.Code != 200 {
		t.Fatal("password login could not enroll", enrolledAgain.Body.String())
	}
	for _, cookie := range enrolledAgain.Result().Cookies() {
		if cookie.Name == auth.DeviceCookie {
			credential = cookie
		}
	}
	loggedOut := call("POST", "/api/auth/logout", `{}`, "192.168.1.2:1234", credential, false)
	if loggedOut.Code != 200 {
		t.Fatal("device logout failed")
	}
	status = call("GET", "/api/auth/status", "", "192.168.1.2:1234", credential, false)
	if !strings.Contains(status.Body.String(), `"logged_in":false`) {
		t.Fatal("logout credential remained usable")
	}

}
