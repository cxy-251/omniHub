package games

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexedDirectoryIgnoresDisplayNameAndRejectsAmbiguity(t *testing.T) {
	root := t.TempDir()
	relative := "unity_games/001 - Physical Folder"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(relative)), 0755); err != nil {
		t.Fatal(err)
	}
	game := &Game{ID: "001", Name: "Completely different title", Category: "unity", RelDir: relative}
	if path, err := ResolveIndexedDirectory(context.Background(), root, game); err != nil || path != relative {
		t.Fatalf("indexed path: %q %v", path, err)
	}
	game.RelDir = ""
	if path, err := ResolveIndexedDirectory(context.Background(), root, game); err != nil || path != relative {
		t.Fatalf("legacy ID path: %q %v", path, err)
	}
	if err := os.Mkdir(filepath.Join(root, "unity_games", "001 - Duplicate"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveIndexedDirectory(context.Background(), root, game); err == nil {
		t.Fatal("ambiguous prefix guessed a directory")
	}
	game.RelDir = "unity_games/../outside"
	if _, err := ResolveIndexedDirectory(context.Background(), root, game); err == nil {
		t.Fatal("escaping indexed path accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolveIndexedDirectory(ctx, root, game); err == nil {
		t.Fatal("cancelled lookup continued")
	}
}

func TestReadOnlyLegacyGameIndexDoesNotMigrateAndWritableUpgradePreservesStats(t *testing.T) {
	root := t.TempDir()
	db, err := OpenDB(root)
	if err != nil {
		t.Fatal(err)
	}
	game := &Game{ID: "001", Name: "Title", Category: "unity", Target: "gui_only", PeakRSSMB: 12, PlayTimeSec: 99, RelDir: "unity_games/001 - Title"}
	if err := db.UpsertGame(game); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("ALTER TABLE games DROP COLUMN rel_dir"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "games.db")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	readonly, err := OpenDBReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	value, err := readonly.GetGame(context.Background(), "001")
	if err != nil || value.RelDir != "" || value.PlayTimeSec != 99 {
		t.Fatalf("legacy read: %+v %v", value, err)
	}
	if err := readonly.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only query changed index")
	}
	writable, err := OpenDB(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := writable.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := writable.UpsertGame(game); err != nil {
		t.Fatal(err)
	}
	value, err = writable.GetGame(context.Background(), "001")
	if err != nil || value.RelDir != game.RelDir || value.PlayTimeSec != 99 || value.PeakRSSMB != 12 {
		t.Fatalf("upgrade lost state: %+v %v", value, err)
	}
	var changes int64
	if err := writable.db.QueryRow("SELECT total_changes()").Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if err := writable.UpsertGame(game); err != nil {
		t.Fatal(err)
	}
	var next int64
	if err := writable.db.QueryRow("SELECT total_changes()").Scan(&next); err != nil {
		t.Fatal(err)
	}
	if changes != next {
		t.Fatal("unchanged metadata rewrote index")
	}
}
