package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"omnihub/internal/auth"
	"omnihub/internal/library"
	"os"
	"path/filepath"
	"testing"
)

func TestUnindexedReadOnlyGameLibrariesListAndOpenWithoutWrites(t *testing.T) {
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
	libs := make([]*library.Library, 0, 2)
	for _, content := range []string{"first", "second"} {
		lib, err := manager.AddLibrary(t.TempDir(), false)
		if err != nil {
			t.Fatal(err)
		}
		folder := filepath.Join(lib.RootPath, "standalone_games", "rpg_games", "001 - Actual Folder")
		if err := os.Mkdir(folder, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "index.html"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "omni.json"), []byte(`{"name":"Different display label"}`), 0600); err != nil {
			t.Fatal(err)
		}
		lib, err = manager.MountLibrary(lib.RootPath, false, true)
		if err != nil {
			t.Fatal(err)
		}
		libs = append(libs, lib)
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(manager, users, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	request := func(method, route, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, route, bytes.NewBufferString(body))
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for index, lib := range libs {
		w := request("GET", "/api/games/list?library_id="+url.QueryEscape(lib.ID), "")
		var list []gameItem
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 || len(list) != 1 || list[0].LibraryID != lib.ID || !list[0].ReadOnly {
			t.Fatalf("unindexed list: %d %s", w.Code, w.Body.String())
		}
		w = request("POST", "/api/games/web/open", fmt.Sprintf(`{"library_id":%q,"id":"001","client_id":"tab-%d"}`, lib.ID, index))
		var opened struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &opened); err != nil || w.Code != 201 {
			t.Fatalf("unindexed open: %d %s", w.Code, w.Body.String())
		}
		w = request("GET", opened.URL, "")
		expected := []string{"first", "second"}[index]
		if w.Code != 200 || w.Body.String() != expected {
			t.Fatalf("wrong owning resource: %d %s", w.Code, w.Body.String())
		}
		for _, name := range []string{"games.db", "games.db-wal", "games.db-shm", "logs"} {
			if _, err := os.Stat(filepath.Join(lib.RootPath, "standalone_games", name)); !os.IsNotExist(err) {
				t.Fatalf("read-only discovery created %s", name)
			}
		}
	}
}
