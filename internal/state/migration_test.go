package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"context"
	"omnihub/internal/manga"
	"omnihub/internal/platform"
	"omnihub/internal/storage"
	"omnihub/internal/userstate"
)

func makeState(t *testing.T, dir string) {
	t.Helper()
	db, err := storage.OpenSQLite(filepath.Join(dir, "users.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE proof(value TEXT); INSERT INTO proof VALUES ('accounts preserved')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func readProof(t *testing.T, dir string) {
	t.Helper()
	db, err := storage.OpenSQLite(filepath.Join(dir, "users.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	var value string
	if err := db.QueryRow("SELECT value FROM proof").Scan(&value); err != nil || value != "accounts preserved" {
		t.Fatalf("account content lost: %q, %v", value, err)
	}
}

func TestMigrationMovesDirectoryAndRollsBackFailedCommit(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[failCommit], func(t *testing.T) {
			root := t.TempDir()
			source, destination := filepath.Join(root, "source"), filepath.Join(root, "target")
			makeState(t, source)
			committed := false
			err := Migrate(filepath.Join(root, "journal.json"), Plan{Source: source, Destination: destination}, func() error {
				if failCommit {
					return errors.New("configuration write failed")
				}
				committed = true
				return nil
			})
			if failCommit {
				if err == nil || committed {
					t.Fatal("failed commit not reported")
				}
				readProof(t, source)
				if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed migration left active destination")
				}
			} else {
				if err != nil || !committed {
					t.Fatalf("migration failed: %v", err)
				}
				readProof(t, destination)
				if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("same filesystem migration did not rename")
				}
			}
		})
	}
}

func TestBootstrapMigrationCopiesOnlyGlobalState(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "config"), filepath.Join(root, "primary")
	makeState(t, source)
	positions, err := userstate.Open(filepath.Join(source, "user_state.db"))
	if err != nil {
		t.Fatal(err)
	}
	key := "library:media_library/manga/book.cbz"
	if err := positions.SaveManga(context.Background(), &manga.MangaProgress{UserID: 1, MangaID: key, PageIndex: 5, ScrollOffsetRatio: 0.8}); err != nil {
		t.Fatal(err)
	}
	textKey := "library:media_library/docs/book.epub"
	if err := positions.SaveText(context.Background(), &userstate.TextPosition{UserID: 1, DocumentID: textKey, StartLine: 1, ChapterIndex: 5, FileVersion: "original"}); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	native := userstate.NativePosition{ID: strings.Repeat("a", 64), Version: strings.Repeat("b", 64), Position: 12.5, Duration: 90, Paused: true, Volume: 42, Rate: 1.5}
	if err := positions.SaveNative(context.Background(), 1, native); err != nil {
		t.Fatal(err)
	}
	if err := positions.RecordGamePlay(context.Background(), 1, "001", started, started.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := positions.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "config.json"), []byte("retain configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(filepath.Join(root, "journal.json"), Plan{Source: source, Destination: destination, Bootstrap: true}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	readProof(t, source)
	readProof(t, destination)
	copied, err := userstate.Open(filepath.Join(destination, "user_state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	resumed, err := copied.Native(context.Background(), 1, native.ID, native.Version)
	if err != nil || resumed == nil || resumed.Position != 12.5 || resumed.Volume != 42 {
		t.Fatalf("native resume not migrated: %+v %v", resumed, err)
	}
	p, err := copied.Manga(context.Background(), 1, key)
	if err != nil || p == nil || p.ScrollOffsetRatio != 0.8 {
		t.Fatal("user reading state not migrated")
	}
	textPosition, err := copied.Text(context.Background(), 1, textKey)
	if err != nil || textPosition == nil || textPosition.ChapterIndex != 5 {
		t.Fatal("text reading state not migrated")
	}
	gameStats, err := copied.GameStats(context.Background(), 1, []string{"001"})
	if err != nil || gameStats["001"].PlayTimeSec != 3 || !gameStats["001"].LastPlayedAt.Equal(started) {
		t.Fatalf("account game statistics not migrated: %+v %v", gameStats, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("copied application configuration into global state")
	}
}

func TestRecoveryAfterRenameBeforeConfigCommit(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "source"), filepath.Join(root, "target")
	makeState(t, source)
	plan := Plan{Source: source, Destination: destination}
	files, err := inspect(plan)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "journal.json")
	if err := saveJournal(journalPath, journal{Plan: plan, Mode: "rename", Files: files}); err != nil {
		t.Fatal(err)
	}
	if err := platform.RenameNoReplace(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := Recover(journalPath, source); err != nil {
		t.Fatal(err)
	}
	readProof(t, source)
	if err := Recover(journalPath, source); err != nil {
		t.Fatal("recovery not idempotent")
	}
}

func TestMigrationRejectsExistingStateAndCorruptDatabase(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "source"), filepath.Join(root, "target")
	makeState(t, source)
	makeState(t, destination)
	committed := false
	if err := Migrate(filepath.Join(root, "journal.json"), Plan{Source: source, Destination: destination}, func() error { committed = true; return nil }); err == nil || committed {
		t.Fatal("existing target overwritten")
	}
	readProof(t, source)
	readProof(t, destination)
	broken := filepath.Join(root, "broken")
	if err := os.Mkdir(broken, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "users.db"), []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(filepath.Join(root, "journal.json"), Plan{Source: broken, Destination: filepath.Join(root, "new")}, func() error { return nil }); err == nil {
		t.Fatal("corrupt state migrated")
	}
}
