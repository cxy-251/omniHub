package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"omnihub/internal/auth"
)

func TestPasswordLoginPersistsAndLogoutClearsSession(t *testing.T) {
	db, err := auth.OpenDB(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateUser("admin", "password", auth.RoleAdmin, auth.LevelAdmin); err != nil {
		t.Fatal(err)
	}
	router := &Router{authDB: db, webGames: newWebGameGrants()}
	req := httptest.NewRequest(http.MethodPost, "https://example.test/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"password"}`))
	response := httptest.NewRecorder()
	router.handleAuthLogin(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("login failed: %d", response.Code)
	}
	var session *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "omni_token" {
			session = cookie
		}
	}
	if session == nil || session.MaxAge < int((db.SessionIdleTimeout()-time.Minute).Seconds()) || session.Expires.Before(time.Now().Add(db.SessionIdleTimeout()-time.Minute)) || !session.HttpOnly || !session.Secure {
		t.Fatalf("login did not persist for idle lifetime: %v", session)
	}
	// A still-valid old session renews before the logout handler clears it.
	if err := db.CreateSession(1, "legacy-session", time.Hour); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "https://example.test/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "omni_token", Value: "legacy-session"})
	response = httptest.NewRecorder()
	auth.NewMiddleware(db, "local-secret").Handler(http.HandlerFunc(router.handleLogout)).ServeHTTP(response, req)
	var last *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "omni_token" {
			last = cookie
		}
	}
	if response.Code != http.StatusOK || last == nil || last.MaxAge != -1 {
		t.Fatalf("renewal interfered with logout: status=%d cookie=%v", response.Code, last)
	}
	if user, err := db.GetUserByToken("legacy-session"); err != nil || user != nil {
		t.Fatalf("logged-out session remains usable: %v, %v", user, err)
	}
}
