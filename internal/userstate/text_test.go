package userstate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestTextPositionsSkipUnchangedWritesAndValidateCursor(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "user_state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := TextPosition{UserID: 1, DocumentID: "library:media_library/docs/a.txt", StartLine: 101, ByteOffset: 400, FileVersion: "current"}
	if err := db.SaveText(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveText(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("unchanged text position rewrote SQLite row")
	}
	p.ByteOffset = -1
	if err := db.SaveText(context.Background(), &p); !errors.Is(err, ErrInvalidProgress) {
		t.Fatal("negative byte cursor accepted")
	}
	p.ByteOffset = 0
	p.ChapterIndex = 20001
	if err := db.SaveText(context.Background(), &p); !errors.Is(err, ErrInvalidProgress) {
		t.Fatal("unbounded chapter accepted")
	}
}
