package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omnihub/internal/auth"
	"omnihub/internal/library"
)

func TestVideoChannelsAndRandomRangeAcrossLibrariesWithoutPrimary(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := library.NewManager()
	contents := make(map[string]string)
	for _, content := range []string{"first-video", "second-video"} {
		root := t.TempDir()
		lib, err := manager.AddLibrary(root, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"common", content + `<quoted>"`} {
			dir := filepath.Join(root, "media_library", "shortvideo", name)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "same.mp4"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		contents[lib.ID] = content
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	r, err := NewRouter(manager, db, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	request := func(path, rangeHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := request("/api/media/shortvideo/channels", "")
	var channels []string
	if err := json.Unmarshal(w.Body.Bytes(), &channels); err != nil || len(channels) != 3 {
		t.Fatalf("merged channels: %d %s", w.Code, w.Body.String())
	}
	for _, lib := range manager.ListLibraries() {
		w = request("/api/media/shortvideo/random?channel=common&library_id="+url.QueryEscape(lib.ID), "")
		var selected struct {
			Path      string `json:"path"`
			LibraryID string `json:"library_id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &selected); err != nil || selected.LibraryID != lib.ID || !strings.HasPrefix(selected.Path, lib.ID+":") {
			t.Fatalf("wrong selected medium: %s", w.Body.String())
		}
		w = request("/api/media/shortvideo/stream?path="+url.QueryEscape(selected.Path), "bytes=1-3")
		if w.Code != http.StatusPartialContent || w.Body.String() != contents[lib.ID][1:4] {
			t.Fatalf("wrong range: %d %s", w.Code, w.Body.String())
		}
		for _, name := range []string{"albums.db", "thumbnails.db", "creators.db"} {
			if _, err := os.Stat(filepath.Join(lib.RootPath, "media_library", "shortvideo", name)); !os.IsNotExist(err) {
				t.Fatal("browsing created index")
			}
		}
	}
	w = request("/api/media/shortvideo/random?channel=common", "")
	if w.Code != 200 {
		t.Fatalf("no-primary random failed: %s", w.Body.String())
	}
	if w := request("/api/media/shortvideo/random?channel=common%2F..", ""); w.Code != 400 {
		t.Fatal("channel traversal accepted")
	}
	if w := request("/api/media/shortvideo/random?channel=common&library_id=unknown", ""); w.Code != 404 {
		t.Fatal("unknown medium substituted")
	}
}
