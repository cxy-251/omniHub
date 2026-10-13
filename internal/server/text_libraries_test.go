package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"omnihub/internal/auth"
	"omnihub/internal/library"
)

func TestTextMultiLibraryPagingAndReadsWithoutPrimary(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := library.NewManager()
	r, err := NewRouter(manager, db, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, text := range []string{"First library\n", "Second library\n"} {
		root := t.TempDir()
		if _, err := manager.AddLibrary(root, false); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "media_library", "docs", "same.txt"), []byte(text), 0600); err != nil {
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
	var first, second []textItem
	w := request("/api/media/text/list?limit=1")
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || len(first) != 1 || w.Header().Get("X-Has-More") != "true" {
		t.Fatalf("first page: %s", w.Body.String())
	}
	w = request("/api/media/text/list?limit=1&offset=1")
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil || len(second) != 1 || first[0].LibraryID == second[0].LibraryID {
		t.Fatalf("second page: %s", w.Body.String())
	}
	a := request("/api/media/text/chunk?path=" + url.QueryEscape(first[0].RelPath) + "&limit=1")
	b := request("/api/media/text/chunk?path=" + url.QueryEscape(second[0].RelPath) + "&limit=1")
	if a.Code != 200 || b.Code != 200 || a.Body.String() == b.Body.String() {
		t.Fatal("same-named texts routed to same library")
	}
	if w := request("/api/media/text/list?limit=10000"); w.Code != 400 {
		t.Fatal("unbounded page accepted")
	}
}
