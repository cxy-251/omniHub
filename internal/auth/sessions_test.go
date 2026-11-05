package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionIdleRenewal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	user, err := db.CreateUser("admin", "password", RoleAdmin, LevelAdmin)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := db.Authenticate("127.0.0.1", user.Username, "password")
	if err != nil {
		t.Fatal(err)
	}
	token := "retained-password-session"
	if err := db.CreateVerifiedSession(context.Background(), proof, token, db.SessionIdleTimeout()); err != nil {
		t.Fatal(err)
	}
	// Existing 24-hour sessions must upgrade on use, while passive checks do not.
	oldExpiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if _, err := db.db.Exec("UPDATE user_sessions SET expires_at=?", oldExpiry.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetUserByToken(token); err != nil || got == nil {
		t.Fatalf("passive validation: user=%v err=%v", got, err)
	}
	var stored string
	if err := db.db.QueryRow("SELECT expires_at FROM user_sessions").Scan(&stored); err != nil || stored != oldExpiry.Format(time.RFC3339) {
		t.Fatalf("passive check changed expiry: %s, %v", stored, err)
	}
	handler := NewMiddleware(db, "local-secret").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()).ID != user.ID {
			t.Error("valid browser session became guest")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "https://example.test/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "omni_token", Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != token || cookies[0].MaxAge < int((db.SessionIdleTimeout()-time.Minute).Seconds()) || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("missing persistent renewed cookie: %v", cookies)
	}
	if err := db.db.QueryRow("SELECT expires_at FROM user_sessions").Scan(&stored); err != nil || cookies[0].Expires.Format(time.RFC3339) != stored {
		t.Fatalf("cookie and database deadlines differ: %s, %v", stored, err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("every media request unnecessarily renews the cookie")
	}
	if err := db.SetSessionIdleDays(context.Background(), 90); err != nil {
		t.Fatal(err)
	}
	_, extended, err := db.UseSession(context.Background(), HashToken(token))
	if err != nil || extended.Before(time.Now().Add(89*24*time.Hour)) {
		t.Fatalf("saved policy not applied to renewal: %v, %v", extended, err)
	}
	for _, days := range []int{0, -1, 366} {
		if err := db.SetSessionIdleDays(context.Background(), days); err != ErrAccountInput {
			t.Fatalf("accepted invalid days %d: %v", days, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetUserByToken(token); err != nil || got == nil || got.ID != user.ID {
		t.Fatalf("session lost after restart: user=%v err=%v", got, err)
	}
	if db.SessionIdleDays() != 90 {
		t.Fatal("saved inactivity limit lost after restart")
	}
}

func TestSessionRenewalCannotRestoreInvalidCredentials(t *testing.T) {
	for _, change := range []string{"expired", "revoked", "password", "disabled"} {
		t.Run(change, func(t *testing.T) {
			db, err := OpenDB(filepath.Join(t.TempDir(), "users.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.CreateUser("admin", "password", RoleAdmin, LevelAdmin); err != nil {
				t.Fatal(err)
			}
			user, err := db.CreateUser("reader", "password", RoleUser, LevelVIP1)
			if err != nil {
				t.Fatal(err)
			}
			const token = "invalidated-session"
			if err := db.CreateSession(user.ID, token, time.Hour); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "expired":
				_, err = db.db.Exec("UPDATE user_sessions SET expires_at=?", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339))
			case "revoked":
				err = db.RevokeUserSessions(user.ID)
			case "password":
				err = db.SetPassword(context.Background(), user.ID, "new-password")
			case "disabled":
				err = db.SetUserStatus(context.Background(), user.ID, "disabled")
			}
			if err != nil {
				t.Fatal(err)
			}
			got, expires, err := db.UseSession(context.Background(), HashToken(token))
			if err != nil || got != nil || !expires.IsZero() {
				t.Fatalf("invalid credential renewed: user=%v expires=%v err=%v", got, expires, err)
			}
		})
	}
}
