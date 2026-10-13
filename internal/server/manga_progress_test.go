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
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/manga"
)

func TestReadOnlyMangaProgressGlobalStorageLegacyAndAccountIsolation(t *testing.T) {
	state := t.TempDir()
	authDB, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()
	owner, err := authDB.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	other, err := authDB.CreateUser("reader", "test-password", "user", 99)
	if err != nil {
		t.Fatal(err)
	}
	if err := authDB.CreateSession(other.ID, "reader-session", time.Hour); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	var keys []string
	var oldIndex string
	for i := 0; i < 2; i++ {
		root := t.TempDir()
		if _, err := library.NewManager().AddLibrary(root, false); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, "media_library", "manga")
		f, err := os.Create(filepath.Join(dir, "same.cbz"))
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		page, err := z.Create("1.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := page.Write([]byte("page")); err != nil {
			t.Fatal(err)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			index, err := manga.OpenDB(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := index.UpsertManga(&manga.Manga{ID: "old-index-id", Title: "Book", RelPath: "same.cbz"}); err != nil {
				t.Fatal(err)
			}
			if err := index.SaveProgress(&manga.MangaProgress{UserID: owner.ID, MangaID: "old-index-id", PageIndex: 5, ScrollOffsetRatio: 0.25}); err != nil {
				t.Fatal(err)
			}
			if err := index.Close(); err != nil {
				t.Fatal(err)
			}
			oldIndex = filepath.Join(dir, "manga.db")
		}
		lib, err := manager.MountLibrary(root, false, true)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, lib.ID+":media_library/manga/same.cbz")
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(oldIndex)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRouter(manager, authDB, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	request := func(method, path string, body any, reader bool) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.RemoteAddr = "127.0.0.1:1234"
		if reader {
			req.Header.Set("Authorization", "Bearer reader-session")
		} else {
			req.Header.Set("X-Omni-Local-Token", "token")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	get := func(key string, reader bool) *httptest.ResponseRecorder {
		return request("GET", "/api/media/manga/progress?manga_id="+url.QueryEscape(key), nil, reader)
	}
	var p manga.MangaProgress
	w := get(keys[0], false)
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.ScrollOffsetRatio != 0.25 || p.MangaID != keys[0] {
		t.Fatalf("legacy progress lost: %d %s", w.Code, w.Body.String())
	}
	for _, key := range keys {
		w = request("POST", "/api/media/manga/progress", map[string]any{"manga_id": key, "page_index": 3, "scroll_offset_ratio": 0.7}, false)
		if w.Code != 200 {
			t.Fatalf("read-only progress failed: %d %s", w.Code, w.Body.String())
		}
		w = get(key, false)
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.ScrollOffsetRatio != 0.7 {
			t.Fatal("global position lost")
		}
	}
	w = get(keys[0], true)
	if w.Code != 200 || w.Body.String() != "null\n" {
		t.Fatalf("account progress leaked: %s", w.Body.String())
	}
	w = request("POST", "/api/media/manga/progress", map[string]any{"user_id": owner.ID, "manga_id": keys[0], "page_index": 1, "scroll_offset_ratio": 0.1}, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = get(keys[0], false)
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.ScrollOffsetRatio != 0.7 {
		t.Fatal("forged user_id overwrote owner's position")
	}
	w = request("POST", "/api/media/manga/progress", map[string]any{"manga_id": keys[0], "page_index": 20001}, false)
	if w.Code != 400 {
		t.Fatal("unbounded page accepted")
	}
	after, err := os.ReadFile(oldIndex)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("legacy media index was modified")
	}
	secondDir := manager.ListLibraries()
	for _, lib := range secondDir {
		if lib.ID+":media_library/manga/same.cbz" == keys[1] {
			if _, err := os.Stat(filepath.Join(lib.RootPath, "media_library", "manga", "manga.db")); !os.IsNotExist(err) {
				t.Fatal("progress created a media index")
			}
		}
	}
}
