package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/manga"
)

func TestMangaCustomCoverUsesOwningIndexAndPreservesArchive(t *testing.T) {
	state := t.TempDir()
	authDB, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()
	if _, err := authDB.CreateInitialAdmin("owner", "test-password"); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	var keys, paths []string
	for i := 0; i < 2; i++ {
		root := t.TempDir()
		if _, err := library.NewManager().AddLibrary(root, false); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, "media_library", "manga")
		path := filepath.Join(dir, "same.cbz")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		for name, body := range map[string]string{"cover.jpg": "default cover", "02.jpg": "selected cover"} {
			entry, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		db, err := manga.OpenDB(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertManga(&manga.Manga{ID: "indexed-id", Title: "Book", RelPath: "same.cbz"}); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		lib, err := manager.MountLibrary(root, false, i == 1)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, lib.ID+":media_library/manga/same.cbz")
		paths = append(paths, path)
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	r, err := NewRouter(manager, authDB, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	request := func(method, path string, data any) *httptest.ResponseRecorder {
		body, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	before, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/api/media/manga/custom-cover", map[string]string{"path": keys[0], "entry": "02.jpg"}); w.Code != 200 {
		t.Fatalf("set cover: %d %s", w.Code, w.Body.String())
	}
	for i, key := range keys {
		w := request("GET", "/api/media/manga/cover?path="+url.QueryEscape(key), nil)
		expected := "selected cover"
		if i == 1 {
			expected = "default cover"
		}
		if w.Code != 200 || w.Body.String() != expected {
			t.Fatalf("incorrect indexed cover: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request("POST", "/api/media/manga/custom-cover", map[string]string{"path": keys[1], "entry": "02.jpg"}); w.Code != 403 {
		t.Fatal("read-only index changed")
	}
	if w := request("POST", "/api/media/manga/custom-cover", map[string]string{"path": keys[0], "entry": "missing.jpg"}); w.Code != 400 {
		t.Fatal("nonexistent page set as cover")
	}
	if w := request("POST", "/api/media/manga/custom-cover", map[string]string{"path": keys[0], "entry": "metadata.json"}); w.Code != 400 {
		t.Fatal("non-image set as cover")
	}
	if w := request("POST", "/api/media/manga/custom-cover", map[string]string{"path": keys[0], "entry": ""}); w.Code != 200 {
		t.Fatal("cover reset failed")
	}
	w := request("GET", "/api/media/manga/cover?path="+url.QueryEscape(keys[0]), nil)
	if w.Body.String() != "default cover" {
		t.Fatal("reset did not restore fallback")
	}
	after, err := os.ReadFile(paths[0])
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("setting cover repacked or modified CBZ")
	}
}
