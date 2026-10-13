package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/gallery"
	"omnihub/internal/library"
)

func TestGalleryAggregationOwnershipAndReadOnlyTrash(t *testing.T) {
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
	contents := make(map[string]string)
	for i, content := range []string{"first", "second"} {
		root := t.TempDir()
		if _, err := library.NewManager().AddLibrary(root, false); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, "media_library", "gallery")
		meta, err := gallery.OpenMetaDB(dir)
		if err != nil {
			t.Fatal(err)
		}
		vm, err := gallery.NewVolumeManager(filepath.Join(dir, "originals"))
		if err != nil {
			t.Fatal(err)
		}
		vol, err := vm.WriteOriginal("same", []byte(content))
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.Close(); err != nil {
			t.Fatal(err)
		}
		if err := meta.UpsertPhoto(&gallery.Photo{ID: "same", Filename: "<img onerror=alert(1)>.jpg", SizeBytes: int64(len(content)), Volume: vol, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		if err := meta.SaveThumbnail(&gallery.Thumbnail{PhotoID: "same", SizeType: "m", Data: []byte("thumb-" + content)}); err != nil {
			t.Fatal(err)
		}
		if err := meta.Close(); err != nil {
			t.Fatal(err)
		}
		lib, err := manager.MountLibrary(root, false, i == 0)
		if err != nil {
			t.Fatal(err)
		}
		contents[lib.ID] = content
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	r, err := NewRouter(manager, authDB, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := request("GET", "/api/media/gallery/list?limit=1")
	var first, second []photoItem
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || len(first) != 1 || w.Header().Get("X-Has-More") != "true" {
		t.Fatalf("first page: %d %s", w.Code, w.Body.String())
	}
	w = request("GET", "/api/media/gallery/list?limit=1&offset=1")
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil || len(second) != 1 || first[0].LibraryID == second[0].LibraryID {
		t.Fatalf("second page: %s", w.Body.String())
	}
	for _, photo := range []photoItem{first[0], second[0]} {
		query := url.Values{"id": {photo.ID}, "library_id": {photo.LibraryID}}.Encode()
		w = request("GET", "/api/media/gallery/original?"+query)
		if w.Code != 200 || w.Body.String() != contents[photo.LibraryID] {
			t.Fatalf("wrong original: %d %s", w.Code, w.Body.String())
		}
		w = request("GET", "/api/media/gallery/thumbnail?"+query)
		if w.Code != 200 || w.Body.String() != "thumb-"+contents[photo.LibraryID] {
			t.Fatal("wrong thumbnail")
		}
		w = request("GET", "/api/media/gallery/original?"+query+"&vol=originals_vol99.db")
		if w.Code != 400 {
			t.Fatal("client selected incorrect volume")
		}
		w = request("POST", "/api/media/gallery/trash?"+query)
		if photo.ReadOnly && w.Code != 403 || !photo.ReadOnly && w.Code != 200 {
			t.Fatalf("trash mode not enforced: readonly=%t code=%d", photo.ReadOnly, w.Code)
		}
	}
	w = request("GET", "/api/media/gallery/list")
	var visible []photoItem
	if err := json.Unmarshal(w.Body.Bytes(), &visible); err != nil || len(visible) != 1 || !visible[0].ReadOnly {
		t.Fatal("trash filter incorrect")
	}
	w = request("GET", "/api/media/gallery/list?include_deleted=1")
	if err := json.Unmarshal(w.Body.Bytes(), &visible); err != nil || len(visible) != 2 {
		t.Fatal("trash entries missing")
	}
	for _, photo := range visible {
		if !photo.ReadOnly {
			query := url.Values{"id": {photo.ID}, "library_id": {photo.LibraryID}}.Encode()
			if w := request("POST", "/api/media/gallery/trash?"+query+"&restore=1"); w.Code != 200 {
				t.Fatal("restore failed")
			}
		}
	}
	if w := request("GET", "/api/media/gallery/list?limit=10000"); w.Code != 400 {
		t.Fatal("unbounded list accepted")
	}
	if w := request("GET", "/api/media/gallery/original?id=same&library_id=unknown"); w.Code != 404 {
		t.Fatal("unknown library substituted")
	}
	// A valid indexed volume name cannot make the original/sidecars follow a link outside its medium.
	lib := manager.ListLibraries()[0]
	link := filepath.Join(lib.RootPath, "media_library", "gallery", "originals", "originals_vol01.db-shm")
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	w = request(http.MethodGet, "/api/media/gallery/original?id=same&library_id="+url.QueryEscape(lib.ID))
	if w.Code != 503 {
		t.Fatalf("volume sidecar escaped: %d %s", w.Code, w.Body.String())
	}
}
