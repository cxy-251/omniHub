package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteSpecialPathsAndReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "索引 & ? #.db")
	db, err := OpenSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE assets (id TEXT PRIMARY KEY); INSERT INTO assets VALUES ('preserved')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenSQLite(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT id FROM assets").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("read: %q, %v", value, err)
	}
	if _, err := db.Exec("INSERT INTO assets VALUES ('forbidden')"); err == nil {
		t.Fatal("read-only DB accepted a write")
	}
}

func TestReadOnlyMissingDatabaseCreatesNothing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing", "meta.db")
	if db, err := OpenSQLite(path, true); err == nil {
		db.Close()
		t.Fatal("missing DB opened")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("read-only open created parent directory: %v", err)
	}
}
