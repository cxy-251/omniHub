package manga

import (
	"context"
	"testing"
)

func TestIndexedPathsPreserveIdentityCoverAndSkipUnchangedWrites(t *testing.T) {
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	legacy := &Manga{ID: "legacy-id", Title: "old title", RelPath: "first/book.cbz", TotalPages: 1}
	if err = db.UpsertManga(legacy); err != nil {
		t.Fatal(err)
	}
	if err = db.SetCustomCover("legacy-id", "chapter/2.jpg"); err != nil {
		t.Fatal(err)
	}
	first := &Manga{ID: "shared-metadata-id", Title: "Book", RelPath: "first/book.cbz", TotalPages: 2}
	second := &Manga{ID: "shared-metadata-id", Title: "Book", RelPath: "second/book.cbz", TotalPages: 2}
	for _, book := range []*Manga{first, second} {
		if err = db.UpsertIndexed(context.Background(), book); err != nil {
			t.Fatal(err)
		}
	}
	if first.ID != "legacy-id" || first.ID == second.ID {
		t.Fatal("path identity collided or legacy identity lost")
	}
	rows, err := db.ListMangasPage(10, 0)
	if err != nil || len(rows) != 2 {
		t.Fatal("same basenames collapsed")
	}
	var cover string
	if err = db.db.QueryRow("SELECT custom_cover FROM mangas WHERE id='legacy-id'").Scan(&cover); err != nil || cover != "chapter/2.jpg" {
		t.Fatal("manual cover reset")
	}
	var before, after int
	if err = db.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = db.UpsertIndexed(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err = db.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil || after != before {
		t.Fatal("unchanged metadata caused writes")
	}
	if err = db.UpsertManga(&Manga{ID: "duplicate", Title: "duplicate", RelPath: first.RelPath}); err != nil {
		t.Fatal(err)
	}
	if err = db.UpsertIndexed(context.Background(), first); err == nil {
		t.Fatal("ambiguous legacy paths silently rewritten")
	}
}
