package userstate

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"omnihub/internal/manga"
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
