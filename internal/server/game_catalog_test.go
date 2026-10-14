package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"omnihub/internal/auth"
	"omnihub/internal/games"
	"omnihub/internal/library"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGameCatalogAggregatesDeduplicatesPagesAndKeepsReadsPure(t *testing.T) {
	state := t.TempDir()
	authDB, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authDB.Close(); err != nil {
			t.Error(err)
		}
	})
	owner, err := authDB.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := authDB.CreateSession(owner.ID, "web-owner", time.Hour); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	first, err := manager.AddLibrary(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	primary, err := manager.AddLibrary(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := manager.AddLibrary(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	originals := make(map[string][]byte)
	for _, lib := range []*library.Library{first, primary} {
		directory := filepath.Join(lib.RootPath, "standalone_games")
		db, err := games.OpenDB(directory)
		if err != nil {
			t.Fatal(err)
		}
		prefix := "First"
		if lib.ID == primary.ID {
			prefix = "Primary"
		}
		list := []*games.Game{{ID: "001", Name: prefix + " copy", Category: "unity", Target: "gui_only", Args: `["private argument"]`, Env: `{"KEY":"private value"}`}, {ID: "002", Name: prefix + " web", Category: "retro", Target: "web_only"}}
		if lib.ID == first.ID {
			list = append(list, &games.Game{ID: "003", Name: "Other native", Category: "godot", Target: "gui_only"})
		}
		if err := db.UpsertGames(list); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		index := filepath.Join(directory, "games.db")
		data, err := os.ReadFile(index)
		if err != nil {
			t.Fatal(err)
		}
		originals[index] = data
	}
	router, err := NewRouter(manager, authDB, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	request := func(query string, native bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/games/list"+query, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.AddCookie(&http.Cookie{Name: "omni_token", Value: "web-owner"})
		if native {
			req.Header.Set("X-Omni-Local-Token", "native-token")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) []gameItem {
		t.Helper()
		var list []gameItem
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 {
			t.Fatalf("list failed: %d %s %v", w.Code, w.Body.String(), err)
		}
		return list
	}
	w := request("?limit=1&target=web_only", true)
	list := decode(w)
	if len(list) != 1 || list[0].ID != "001" || list[0].LibraryID != primary.ID || list[0].Args != "" || list[0].Env != "" || w.Header().Get("X-Has-More") != "true" {
		t.Fatalf("primary/native/page boundary: %+v", list)
	}
	w = request("?limit=1&offset=1", true)
	list = decode(w)
	if len(list) != 1 || list[0].ID != "003" || list[0].LibraryID != first.ID || w.Header().Get("X-Has-More") != "false" {
		t.Fatalf("duplicate shifted second page: %+v", list)
	}
	list = decode(request("?target=gui_only", false))
	if len(list) != 1 || list[0].ID != "002" || list[0].Target != "web_only" {
		t.Fatal("forged target exposed native game")
	}
	list = decode(request("?library_id="+url.QueryEscape(first.ID), true))
	if len(list) != 2 || list[0].LibraryID != first.ID {
		t.Fatal("explicit library lost ownership")
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	list = decode(request("?category=godot", true))
	if len(list) != 1 || list[0].ID != "003" {
		t.Fatal("no-primary catalog lost secondary game")
	}
	manager.SetCategoryDisabled("standalone_games/godot_games", true)
	list = decode(request("?category=godot", true))
	if len(list) != 0 {
		t.Fatal("disabled category remained visible")
	}
	for _, query := range []string{"?limit=101", "?offset=-1", "?category=../outside", "?library_id=missing"} {
		if w := request(query, true); w.Code != 400 && w.Code != 404 {
			t.Fatalf("invalid catalog query accepted: %s %d", query, w.Code)
		}
	}
	for index, before := range originals {
		after, err := os.ReadFile(index)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("catalog browse mutated index")
		}
	}
	if _, err := os.Stat(filepath.Join(empty.RootPath, "standalone_games", "games.db")); !os.IsNotExist(err) {
		t.Fatal("browse created missing index")
	}
}
