package userstate

import (
	"context"
	"path/filepath"
	"testing"

	"omnihub/internal/manga"
)

func TestLargeMangaCheckpointSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := &manga.MangaProgress{UserID: 1, MangaID: "fixture:media_library/manga/standard/large.cbz", PageIndex: 200001, ChapterIndex: 24000, PositionMode: "page", FileVersion: "large-v1", ScrollOffsetRatio: 0.4}
	if err := db.SaveManga(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	p.PageIndex = manga.MaxPages
	if err := db.SaveManga(context.Background(), p); err != ErrInvalidProgress {
		t.Fatal("accepted out-of-range page", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.Manga(context.Background(), 1, p.MangaID)
	if err != nil || got == nil || got.PageIndex != 200001 || got.ChapterIndex != 24000 || got.ScrollOffsetRatio != 0.4 {
		t.Fatalf("large checkpoint lost or overwritten: %+v, %v", got, err)
	}
}
