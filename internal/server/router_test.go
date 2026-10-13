package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"omnihub/internal/auth"
	"omnihub/internal/library"
)

func TestModuleConnectionsStayWithTheirLibrary(t *testing.T) {
	state := t.TempDir()
	authDB, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()
	r, err := NewRouter(library.NewManager(), authDB, state, "test-local-token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rootA, rootB := t.TempDir(), t.TempDir()
	mangaA, err := r.getMangaDB(rootA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.getMetaDB(rootB); err != nil {
		t.Fatal(err)
	}
	mangaB, err := r.getMangaDB(rootB)
	if err != nil {
		t.Fatal(err)
	}
	if mangaA == mangaB {
		t.Fatal("manga connection reused across libraries")
	}
	again, err := r.getMangaDB(rootA)
	if err != nil || again != mangaA {
		t.Fatalf("lost cached connection: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.getMangaDB(rootA); err == nil {
		t.Fatal("opened DB after router close")
	}
}

func TestPermissionLookupFailureDoesNotGrantAccess(t *testing.T) {
	db, err := auth.OpenDB(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	r := &Router{authDB: db}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/media/gallery/list", nil)
	if r.checkSectionAccess(w, req, "gallery", auth.TargetAll) {
		t.Fatal("failed lookup granted access")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d", w.Code)
	}
}
