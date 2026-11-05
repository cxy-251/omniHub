package manga

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompletionRequiresExplicitPublicationStatus(t *testing.T) {
	for _, tc := range []struct {
		name, json      string
		known, complete bool
	}{
		{"ordinary without metadata", "", false, false},
		{"title only", `{"title":"Collected edition"}`, false, false},
		{"legacy up-to-date download", `{"is_complete":true,"local_chapters":2,"online_chapters":2}`, false, false},
		{"legacy missing chapters", `{"is_complete":false,"local_chapters":1,"online_chapters":2}`, false, false},
		{"explicit completed", `{"completion_status":"completed"}`, true, true},
		{"explicit ongoing", `{"completion_status":"ongoing"}`, true, false},
		{"explicit unknown overrides legacy bool", `{"completion_status":"unknown","is_complete":true}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "book.cbz")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(file)
			if tc.json != "" {
				entry, err := z.Create("metadata.json")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte(tc.json)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := z.Create("chapter/001.png"); err != nil {
				t.Fatal(err)
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			book, err := ScanMangaFromCBZContext(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if book.CompletionKnown != tc.known || book.IsComplete != tc.complete {
				t.Fatalf("mislabelled publication status: known=%v complete=%v", book.CompletionKnown, book.IsComplete)
			}
		})
	}
}

func TestCompletionMigrationPreservesExistingBook(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	book := &Manga{ID: "retained", Title: "Existing", RelPath: "standard/book.cbz", CustomCover: "custom.png"}
	if err := db.UpsertManga(book); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("ALTER TABLE mangas DROP COLUMN completion_known"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	legacy, err := OpenDBReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	oldBooks, err := legacy.ListAllMangas()
	if err != nil || len(oldBooks) != 1 || oldBooks[0].CompletionKnown {
		t.Fatal("read-only legacy index failed", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	books, err := db.ListAllMangas()
	if err != nil || len(books) != 1 {
		t.Fatalf("migration lost existing index: %v", err)
	}
	if books[0].ID != book.ID || books[0].CustomCover != book.CustomCover || books[0].CompletionKnown {
		t.Fatal("migration changed identity/cover or invented a publication status")
	}
	book.CompletionKnown = true
	if err := db.UpsertManga(book); err != nil {
		t.Fatal(err)
	}
	books, err = db.ListAllMangas()
	if err != nil || len(books) != 1 || !books[0].CompletionKnown || books[0].IsComplete {
		t.Fatal("explicit ongoing status lost", err)
	}
}
