package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"omnihub/internal/auth"
	"omnihub/internal/library"
	"os"
	"path/filepath"
	"testing"
)

func TestHostIndexBuildsEmptyLibraryAndRejectsUntrustedOrReadOnlyWrites(t *testing.T) {
	state := t.TempDir()
	users, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := users.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := users.CreateInitialAdmin("owner", "test-password"); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	root := t.TempDir()
	lib, err := manager.AddLibrary(root, false)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "standalone_games")
	if err := os.Mkdir(filepath.Join(directory, "rpg_games", "001 - Real Folder"), 0755); err != nil {
		t.Fatal(err)
	}
	disabled := filepath.Join(directory, "unity_games", "009 - Disabled")
	if err := os.Mkdir(disabled, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disabled, "omni.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	manager.SetCategoryDisabled("standalone_games/unity_games", true)
	router, err := NewRouter(manager, users, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	body := `{"library_id":"` + lib.ID + `"}`
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.RemoteAddr = "127.0.0.1:1234"
		if token != "" {
			req.Header.Set("X-Omni-Local-Token", token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if w := request("POST", "/api/games/index", body, ""); w.Code != 403 {
		t.Fatal("untrusted scan accepted")
	}
	if _, err := os.Stat(filepath.Join(directory, "games.db")); !os.IsNotExist(err) {
		t.Fatal("rejected scan created index")
	}
	func() {
		router.runtimeMu.Lock()
		defer router.runtimeMu.Unlock()
		if router.CanReconfigure() {
			t.Fatal("index lease allowed migration")
		}
		if w := request("POST", "/api/games/index", body, "native-token"); w.Code != 409 {
			t.Fatal("parallel scan entered")
		}
	}()
	w := request("POST", "/api/games/index", body, "native-token")
	var result struct {
		Indexed int `json:"indexed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result.Indexed != 1 {
		t.Fatalf("first actual scan: %d %s %v", w.Code, w.Body.String(), err)
	}
	// Web browsing consumes the newly built index with no native credential.
	w = request("GET", "/api/games/list", "", "")
	var items []gameItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil || w.Code != 200 || len(items) != 1 || items[0].LibraryID != lib.ID {
		t.Fatalf("indexed web catalog: %d %s", w.Code, w.Body.String())
	}
	before, err := os.ReadFile(filepath.Join(directory, "games.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.MountLibrary(root, false, true); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/api/games/index", body, "native-token"); w.Code != 403 {
		t.Fatal("read-only scan accepted")
	}
	after, err := os.ReadFile(filepath.Join(directory, "games.db"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only scan changed index")
	}
}
