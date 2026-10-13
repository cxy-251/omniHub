package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"omnihub/internal/library"
)

func TestStateLocationPreservesBootstrapAndRejectsAmbiguity(t *testing.T) {
	bootstrap := t.TempDir()
	root := t.TempDir()
	primary := &library.Library{RootPath: root, IsOnline: true}
	if err := os.WriteFile(filepath.Join(bootstrap, "users.db"), []byte("existing accounts"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	dir, changed, err := cfg.ResolveStateDirectory(bootstrap, primary)
	if err != nil || !changed || dir != bootstrap {
		t.Fatalf("lost bootstrap accounts: %q, %v, %v", dir, changed, err)
	}
	other := filepath.Join(root, library.DataDirName)
	if err := os.MkdirAll(other, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "users.db"), []byte("other accounts"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DefaultConfig().ResolveStateDirectory(bootstrap, primary); err == nil {
		t.Fatal("silently selected one of two user databases")
	}
	cfg.StateInitialized = true
	if err := os.Rename(filepath.Join(bootstrap, "users.db"), filepath.Join(bootstrap, "unavailable.db")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cfg.ResolveStateDirectory(bootstrap, primary); err == nil {
		t.Fatal("allowed recreation of unavailable state")
	}
}

func TestPrivateFileReplacesPermissiveCredential(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-client.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivateFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("credential permissions: %v", info.Mode())
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("replacement: %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary transaction leaked: %v, %v", entries, err)
	}
}

func TestLibraryIdentityConfigRoundTrip(t *testing.T) {
	t.Setenv("OMNIHUB_CONFIG_DIR", t.TempDir())
	cfg := DefaultConfig()
	root := t.TempDir()
	lib := &library.Library{ID: "stable-id", Name: "disk", RootPath: root, IsOnline: true}
	if !cfg.RememberLibrary(lib) || cfg.RememberLibrary(lib) {
		t.Fatal("identity changes were not tracked correctly")
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	known := loaded.KnownLibrary(root)
	if known == nil || known.ID != lib.ID {
		t.Fatal("stable identity was not persisted")
	}
	known.ID = "changed"
	if loaded.KnownLibrary(root).ID != lib.ID {
		t.Fatal("query exposed internal identity map")
	}
}
