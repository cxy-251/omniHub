package scanner

import (
	"bytes"
	"context"
	"omnihub/internal/games"
	"os"
	"path/filepath"
	"testing"
)

func TestGameScanPublishesDirectoryAndRejectsBadBatch(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "unity_games", "001 - Physical Folder")
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "omni.json"), []byte(`{"name":"Renamed title","exe":"game.bin"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if count, err := ScanGames(root); err != nil || count != 1 {
		t.Fatalf("scan: %d %v", count, err)
	}
	db, err := games.OpenDBReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	value, err := db.GetGame(context.Background(), "001")
	if err != nil || value.RelDir != "unity_games/001 - Physical Folder" || value.Name != "Renamed title" {
		t.Fatalf("directory/label confused: %+v %v", value, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	duplicate := filepath.Join(root, "godot_games", "001 - Duplicate")
	if err := os.MkdirAll(duplicate, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanGames(root); err == nil {
		t.Fatal("duplicate prefix overwrote a game")
	}
	db, err = games.OpenDBReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	value, err = db.GetGame(context.Background(), "001")
	if err != nil || value.Name != "Renamed title" {
		t.Fatal("failed batch changed existing metadata")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidGameMetadataLeavesIndexBytesUntouched(t *testing.T) {
	for _, metadata := range []string{`{`, `{"exe":"../outside"}`, `{"exe":"C:\\outside.exe"}`, `{"name":"\u0000"}`} {
		t.Run(metadata, func(t *testing.T) {
			root := t.TempDir()
			folder := filepath.Join(root, "3ds_games", "003 - Original")
			if err := os.MkdirAll(folder, 0755); err != nil {
				t.Fatal(err)
			}
			if _, err := ScanGames(root); err != nil {
				t.Fatal(err)
			}
			index := filepath.Join(root, "games.db")
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, "omni.json"), []byte(metadata), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ScanGames(root); err == nil {
				t.Fatal("invalid metadata accepted")
			}
			after, err := os.ReadFile(index)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid scan modified existing database")
			}
		})
	}
}
