package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestLANDeviceScopeAndReadOnlyCredentialValidation(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "users.db"))
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
	device, token, err := db.RegisterDevice(context.Background(), owner.ID, HashToken("proof"), "Tablet", "192.168.1.2")
	if err != nil {
		t.Fatal(err)
	}
	if old, err := db.GetUserByToken("proof"); err != nil || old != nil {
		t.Fatal("converted normal session remained valid")
	}
	if err = db.CreateSession(owner.ID, "rescue", time.Hour); err != nil {
		t.Fatal(err)
	}
	middleware := NewMiddleware(db, "local")
	check := func(addr, header string, want bool) {
		t.Helper()
		request := httptest.NewRequest("GET", "/api/auth/status", nil)
		request.RemoteAddr = addr
		request.AddCookie(&http.Cookie{Name: DeviceCookie, Value: token})
		request.Header.Set("Authorization", "Bearer rescue")
		if header != "" {
			request.Header.Set(header, "")
		}
		middleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if (user.ID == owner.ID) != want {
				t.Fatalf("scope %s / %s: %+v", addr, header, user)
			}
			if want && (AuthenticationMethod(r.Context()) != "device" || DeviceID(r.Context()) != device.ID || ClientFromContext(r.Context()) != TargetWeb || IsTrustedLocalFromContext(r.Context())) {
				t.Fatal("device promoted to native identity or lost proof")
			}
		})).ServeHTTP(httptest.NewRecorder(), request)
	}
	for _, addr := range []string{"10.0.0.1:1234", "172.16.0.1:1234", "172.31.255.254:1234", "192.168.1.3:1234", "[::ffff:192.168.1.3]:1234"} {
		check(addr, "", true)
	}
	for _, addr := range []string{"127.0.0.1:1234", "172.32.0.1:1234", "192.169.0.1:1234", "8.8.8.8:1234", "[fd00::1]:1234", "not-an-ip"} {
		check(addr, "", false)
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "CF-Connecting-IP", "X-Real-IP"} {
		check("192.168.1.3:1234", header, false)
	}
	var before, after int
	if err = db.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if user, _, err := db.TrustedUserByHash(context.Background(), HashToken(token)); err != nil || user == nil {
			t.Fatal("device lookup failed")
		}
	}
	if err = db.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil || after != before {
		t.Fatal("device reads caused persistent writes")
	}
	if _, err = db.db.Exec("UPDATE trusted_devices SET expires_at=? WHERE device_id=?", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), device.ID); err != nil {
		t.Fatal(err)
	}
	check("192.168.1.3:1234", "", false)
	if _, err = db.db.Exec("UPDATE trusted_devices SET expires_at=? WHERE device_id=?", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), device.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateUserLevel(owner.ID, LevelAdmin); err != nil {
		t.Fatal(err)
	}
	check("192.168.1.3:1234", "", false)
}

func TestDeviceCapacityAndLegacyRowsDoNotGrantAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := db.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the original four-column table with a populated legacy record.
	if _, err = db.db.Exec(`DROP TABLE trusted_devices; CREATE TABLE trusted_devices(token_hash TEXT PRIMARY KEY,device_name TEXT NOT NULL,ip TEXT NOT NULL,created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	// Rows from the old, unowned schema must never become administrator tokens.
	legacyToken, err := GenerateSecureToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec("INSERT INTO trusted_devices(token_hash,device_name,ip,created_at) VALUES(?,?,?,?)", HashToken(legacyToken), "legacy", "192.168.1.2", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if user, _, err := db.TrustedUserByHash(context.Background(), HashToken(legacyToken)); err != nil || user != nil {
		t.Fatal("unowned legacy credential gained authority")
	}
	for i := 0; i < MaxTrustedDevices; i++ {
		if _, err = db.db.Exec(`INSERT INTO trusted_devices(token_hash,device_name,ip,created_at,device_id,user_id,expires_at) VALUES(?,?,?,?,?,?,?)`, fmt.Sprintf("%064x", i+1), "fixture", "192.168.1.2", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf("%032x", i+1), owner.ID, time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.CreateSession(owner.ID, "proof", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.RegisterDevice(context.Background(), owner.ID, HashToken("proof"), "overflow", "192.168.1.2"); err == nil {
		t.Fatal("unbounded device registration")
	}
	if user, err := db.GetUserByToken("proof"); err != nil || user == nil {
		t.Fatal("failed enrollment consumed password session")
	}
}
