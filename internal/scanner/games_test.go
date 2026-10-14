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
	for _, metadata := range []string{`{`, `{"exe":"../outside"}`, `{"exe":"C:\\outside.exe"}`, `{"name":"\u0000"}`, `{"args":42}`, `{"args":[null]}`, `{"env":{"OMNI_SAVE_DIR":"override"}}`} {
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

func TestGameScanMissingRestoredAndCancelledSnapshotsPreserveHistory(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "unity_games", "001 - Original")
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanGames(root); err != nil {
		t.Fatal(err)
	}
	db, err := games.OpenDB(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateHardwareProfile("001", 55, 2, 99); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "games.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ScanGames(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, "games.db"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("unchanged scan rewrote main database")
	}
	parked := filepath.Join(t.TempDir(), "parked")
	if err := os.Rename(folder, parked); err != nil {
		t.Fatal(err)
	}
	if count, err := ScanGames(root); err != nil || count != 0 {
		t.Fatalf("missing scan: %d %v", count, err)
	}
	check := func(available bool, name string) {
		t.Helper()
		db, err := games.OpenDBReadOnly(root)
		if err != nil {
			t.Fatal(err)
		}
		value, readErr := db.GetGame(context.Background(), "001")
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if readErr != nil || value.Available != available || value.Name != name || value.PlayTimeSec != 99 || value.PeakRSSMB != 55 {
			t.Fatalf("snapshot lost history: %+v %v", value, readErr)
		}
	}
	check(false, "Original")
	restored := filepath.Join(root, "unity_games", "001 - Restored")
	if err := os.Rename(parked, restored); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanGames(root); err != nil {
		t.Fatal(err)
	}
	check(true, "Restored")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	empty := t.TempDir()
	if _, err := ScanGamesContext(cancelled, empty); err == nil {
		t.Fatal("cancelled scan published")
	}
	if _, err := os.Stat(filepath.Join(empty, "games.db")); !os.IsNotExist(err) {
		t.Fatal("pre-cancelled scan created index")
	}
}
