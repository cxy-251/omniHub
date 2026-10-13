package server

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/manga"
)

func TestMangaAggregationWithoutPrimaryRoutesSameNameToOwningLibrary(t *testing.T) {
	state := t.TempDir()
	authDB, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()
	manager := library.NewManager()
	r, err := NewRouter(manager, authDB, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, content := range []string{"first", "second"} {
		root := t.TempDir()
		if _, err := manager.AddLibrary(root, false); err != nil {
			t.Fatal(err)
		}
		db, err := r.getMangaDB(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertManga(&manga.Manga{ID: "same", Title: "Same title", RelPath: "same.cbz"}); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(root, "media_library", "manga", "same.cbz"))
		if err != nil {
			t.Fatal(err)
		}
		archive := zip.NewWriter(file)
		page, err := archive.Create("1.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := page.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := request("/api/media/manga/list?limit=1")
	var first []mangaItem
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || len(first) != 1 || w.Header().Get("X-Has-More") != "true" {
		t.Fatalf("first page: %d %s", w.Code, w.Body.String())
	}
	w = request("/api/media/manga/list?limit=1&offset=1")
	var second []mangaItem
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil || len(second) != 1 || second[0].LibraryID == first[0].LibraryID {
		t.Fatal("second library missing or identity collision")
	}
	firstPage := request("/api/media/manga/page?path=" + url.QueryEscape(first[0].LogicalPath) + "&entry=1.jpg")
	secondPage := request("/api/media/manga/page?path=" + url.QueryEscape(second[0].LogicalPath) + "&entry=1.jpg")
	if firstPage.Code != 200 || secondPage.Code != 200 || firstPage.Body.String() == secondPage.Body.String() {
		t.Fatal("same-named resources resolved to wrong library")
	}
}

func TestMangaDatabaseCannotEscapeThroughFileOrSidecarLink(t *testing.T) {
	for _, name := range []string{"manga.db", "manga.db-wal", "manga.db-shm"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			manager := library.NewManager()
			if _, err := manager.AddLibrary(root, false); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(outside, "other.db"), filepath.Join(root, "media_library", "manga", name)); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			r := &Router{libMgr: manager}
			if _, err := r.moduleDirectory(root, "media_library/manga", "manga.db"); err == nil {
				t.Fatal("database boundary bypassed")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("external database created")
			}
		})
	}
}
