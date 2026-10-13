package userstate

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"omnihub/internal/manga"
	"omnihub/internal/storage"
)

func TestProgressAccountIsolationBoundsAndUnchangedWrites(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "user_state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	p := manga.MangaProgress{UserID: 1, MangaID: "library:media_library/manga/book.cbz", PageIndex: 5, ScrollOffsetRatio: 0.25}
	if err := db.SaveManga(ctx, &p); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveManga(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("identical position rewrote SQLite pages")
	}
	stored, err := db.Manga(ctx, 1, p.MangaID)
	if err != nil || stored.PositionMode != "book" {
		t.Fatal("legacy position no longer has explicit book semantics")
	}
	p.PositionMode = "page"
	p.FileVersion = "current-version"
	if err := db.SaveManga(ctx, &p); err != nil {
		t.Fatal(err)
	}
	stored, err = db.Manga(ctx, 1, p.MangaID)
	if err != nil || stored.PositionMode != "page" || stored.FileVersion != "current-version" {
		t.Fatal("page-relative position lost semantics/version")
	}
	if other, err := db.Manga(ctx, 2, p.MangaID); err != nil || other != nil {
		t.Fatal("another account received reading position")
	}
	for _, ratio := range []float64{-1, 2, math.NaN(), math.Inf(1)} {
		p.ScrollOffsetRatio = ratio
		if err := db.SaveManga(ctx, &p); !errors.Is(err, ErrInvalidProgress) {
			t.Fatalf("invalid ratio accepted: %v", err)
		}
	}
	p.ScrollOffsetRatio = 0.25
	p.PageIndex = 20001
	if err := db.SaveManga(ctx, &p); !errors.Is(err, ErrInvalidProgress) {
		t.Fatal("unbounded page accepted")
	}
	p.PageIndex = 5
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := db.SaveManga(canceled, &p); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled write completed")
	}
}

func TestMangaProgressSchemaUpgradePreservesOldBookRatio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user_state.db")
	old, err := storage.OpenSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE manga_progress(user_id INTEGER NOT NULL,manga_id TEXT NOT NULL,chapter_index INTEGER NOT NULL,page_index INTEGER NOT NULL,scroll_offset_ratio REAL NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(user_id,manga_id));
		INSERT INTO manga_progress VALUES(1,'library:media_library/manga/book.cbz',0,0,0.65,'2026-10-14T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.Manga(context.Background(), 1, "library:media_library/manga/book.cbz")
	if err != nil || p == nil || p.ScrollOffsetRatio != 0.65 || p.PositionMode != "book" || p.FileVersion != "" {
		t.Fatalf("historical progress changed: %+v %v", p, err)
	}
}
