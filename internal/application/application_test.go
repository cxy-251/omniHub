package application

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/config"
	"omnihub/internal/library"
)

func call(t *testing.T, app *Application, method, path string, body any, trusted bool) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.RemoteAddr = "127.0.0.1:43210"
	if trusted {
		req.Header.Set("X-Omni-Local-Token", "test-token")
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	return w
}

func newTestApplication(t *testing.T) *Application {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OMNIHUB_CONFIG_DIR", dir)
	app, err := New(config.DefaultConfig(), dir, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	if app.current.router == nil {
		t.Fatal(app.current.stateError)
	}
	return app
}

func TestConfirmedPrimaryMigrationPreservesAdminAndBootstrapConfig(t *testing.T) {
	app := newTestApplication(t)
	w := call(t, app, http.MethodPost, "/api/auth/setup", map[string]string{"username": "owner", "password": "test-password"}, true)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	root := t.TempDir()
	w = call(t, app, http.MethodPost, "/api/library/add", map[string]any{"path": root}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("add: %d %s", w.Code, w.Body.String())
	}
	if app.cfg.StateDirectory != app.configDir || app.current.libraries.GetPrimary() != nil {
		t.Fatal("adding library silently changed global state")
	}
	lib := app.current.libraries.ListLibraries()[0]
	w = call(t, app, http.MethodPost, "/api/library/primary/commit", map[string]string{"library_id": lib.ID}, true)
	if w.Code != http.StatusConflict {
		t.Fatal("migration without confirmation accepted")
	}
	w = call(t, app, http.MethodPost, "/api/library/primary/preview", map[string]string{"library_id": lib.ID}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("preview: %s", w.Body.String())
	}
	var preview struct {
		Token string `json:"confirmation_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	w = call(t, app, http.MethodPost, "/api/library/primary/commit", map[string]string{"library_id": lib.ID, "confirmation_token": preview.Token}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("migrate: %d %s", w.Code, w.Body.String())
	}
	if app.cfg.StateDirectory != filepath.Join(root, library.DataDirName) {
		t.Fatal("state not migrated")
	}
	admin, err := app.current.authDB.GetActiveAdmin()
	if err != nil || admin.Username != "owner" {
		t.Fatalf("admin lost: %+v %v", admin, err)
	}
	if _, err := os.Stat(filepath.Join(app.configDir, "config.json")); err != nil {
		t.Fatal("configuration moved out of config directory")
	}
}

func TestOfflineStateKeepsServiceAndRecoversWithoutReplacementAccounts(t *testing.T) {
	app := newTestApplication(t)
	statePath := app.cfg.StateDirectory
	if err := app.pause(true); err != nil {
		t.Fatal(err)
	}
	if err := closeGeneration(app.current); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(statePath, "users.db"), filepath.Join(statePath, "unplugged.db")); err != nil {
		t.Fatal(err)
	}
	app.resume(app.build())
	if app.current.router != nil {
		t.Fatal("replacement state opened")
	}
	if _, err := os.Stat(filepath.Join(statePath, "users.db")); !os.IsNotExist(err) {
		t.Fatal("replacement account file created")
	}
	w := call(t, app, http.MethodGet, "/api/library/manage", nil, false)
	if w.Code != http.StatusForbidden {
		t.Fatal("untrusted offline recovery allowed")
	}
	w = call(t, app, http.MethodGet, "/api/library/manage", nil, true)
	if w.Code != http.StatusOK {
		t.Fatal("local recovery not available")
	}
	w = call(t, app, http.MethodGet, "/api/auth/status", nil, true)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("offline auth pretended healthy")
	}
	if err := os.Rename(filepath.Join(statePath, "unplugged.db"), filepath.Join(statePath, "users.db")); err != nil {
		t.Fatal(err)
	}
	w = call(t, app, http.MethodPost, "/api/library/refresh", nil, true)
	if w.Code != http.StatusOK || app.current.router == nil {
		t.Fatalf("reconnect failed: %s", w.Body.String())
	}
}

func TestLibraryManagementRejectsOriginAndUntrustedLoopback(t *testing.T) {
	app := newTestApplication(t)
	w := call(t, app, http.MethodPost, "/api/library/add", map[string]string{"path": t.TempDir()}, false)
	if w.Code == http.StatusOK {
		t.Fatal("loopback alone authorized configuration")
	}
	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/library/refresh", nil)
	req.RemoteAddr = "127.0.0.1:43210"
	req.Header.Set("X-Omni-Local-Token", "test-token")
	req.Header.Set("Origin", "https://remote.invalid")
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatal("cross-origin management accepted")
	}
}

func TestRemovedStateMediumStillRequiresItsOriginalIdentity(t *testing.T) {
	app := newTestApplication(t)
	base := t.TempDir()
	root := filepath.Join(base, "library")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if w := call(t, app, http.MethodPost, "/api/library/add", map[string]string{"path": root}, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	lib := app.current.libraries.ListLibraries()[0]
	if _, err := app.current.authDB.CreateInitialAdmin("original", "original-password"); err != nil {
		t.Fatal(err)
	}
	if w := call(t, app, http.MethodPost, "/api/library/primary/preview", map[string]string{"library_id": lib.ID}, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := app.migrate(app.confirmation); err != nil {
		t.Fatal(err)
	}
	if w := call(t, app, http.MethodPost, "/api/library/remove", map[string]string{"library_id": lib.ID}, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if app.cfg.StateLibraryID != lib.ID {
		t.Fatal("removal erased global state identity")
	}
	if err := os.Rename(root, filepath.Join(base, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := library.NewManager().AddLibrary(root, true); err != nil {
		t.Fatal(err)
	}
	db, err := auth.OpenDB(filepath.Join(root, library.DataDirName, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInitialAdmin("wrong-medium", "other-password"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := app.checkLibraries(); err != nil {
		t.Fatal(err)
	}
	if app.current.router != nil {
		t.Fatal("opened accounts from replacement medium")
	}
	if err := os.Rename(root, filepath.Join(base, "wrong")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(base, "original"), root); err != nil {
		t.Fatal(err)
	}
	if w := call(t, app, http.MethodPost, "/api/library/refresh", map[string]any{}, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	admin, err := app.current.authDB.GetActiveAdmin()
	if err != nil || admin.Username != "original" {
		t.Fatalf("original accounts not restored: %+v, %v", admin, err)
	}
}

func TestReconfigurationUnblocksSlowRequestBody(t *testing.T) {
	app := newTestApplication(t)
	host := httptest.NewServer(app)
	defer host.Close()
	conn, err := net.DialTimeout("tcp", host.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request := "POST /api/auth/login HTTP/1.1\r\nHost: localhost\r\nX-Omni-Local-Token: test-token\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{\"username\":\"unfinished"
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	active := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		app.mu.Lock()
		active = app.active > 0
		app.mu.Unlock()
		if active {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !active {
		t.Fatal("slow request did not reach application")
	}
	started := time.Now()
	if err := app.pause(false); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("slow request prevented prompt draining")
	}
	app.resumeWithFreshContext(app.current)
}

func TestCategoryConfigurationPersistsAndDisablesModuleRoutes(t *testing.T) {
	app := newTestApplication(t)
	root := t.TempDir()
	if w := call(t, app, http.MethodPost, "/api/library/categories", map[string]any{"directories": []string{"media_library/audio", "media_library/custom"}}, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call(t, app, http.MethodPost, "/api/library/add", map[string]any{"path": root}, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "media_library", "custom")); err != nil {
		t.Fatal("custom category not created")
	}
	if _, err := os.Stat(filepath.Join(root, "media_library", "gallery")); !os.IsNotExist(err) {
		t.Fatal("default disabled category recreated")
	}
	if w := call(t, app, http.MethodGet, "/api/media/gallery/list", nil, true); w.Code != 404 {
		t.Fatal("disabled gallery route not blocked")
	}
	if w := call(t, app, http.MethodPost, "/api/library/categories", map[string]any{"directories": []string{}}, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	loaded, err := config.Load()
	if err != nil || loaded.CategoryDirectories == nil || len(loaded.CategoryDirectories) != 0 {
		t.Fatal("empty configuration was not persisted")
	}
	if w := call(t, app, http.MethodPost, "/api/library/refresh", map[string]any{}, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "media_library", "custom")); err != nil {
		t.Fatal("removed category deleted content container")
	}
	if !app.current.libraries.IsCategoryDisabled("media_library/custom") {
		t.Fatal("custom disable history lost")
	}
}
