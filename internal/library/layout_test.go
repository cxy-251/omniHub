package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredSkeletonPreservesDisabledContentAndBlocksEscapingLinks(t *testing.T) {
	root := t.TempDir()
	manager, err := NewConfiguredManager([]string{"media_library/audio"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.MountLibrary(root, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "media_library", "manga")); !os.IsNotExist(err) {
		t.Fatal("disabled category created")
	}
	if err := os.WriteFile(filepath.Join(root, "media_library", "audio", "track.mp3"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	disabled, err := NewConfiguredManager([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.MountLibrary(root, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.ResolveCategoryPath("media_library/audio/track.mp3", "media_library/audio"); err == nil {
		t.Fatal("disabled asset remained accessible")
	}
	if data, err := os.ReadFile(filepath.Join(root, "media_library", "audio", "track.mp3")); err != nil || string(data) != "keep" {
		t.Fatal("disabled content lost")
	}
	unsafe := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(unsafe, "media_library")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := EnsureSkeleton(unsafe); err == nil {
		t.Fatal("skeleton escaped through symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("skeleton wrote outside resource root")
	}
}

func TestCategoryInputValidation(t *testing.T) {
	for _, dirs := range [][]string{nil, {"../outside"}, {"media_library/audio", "media_library/audio"}, {".omni_data/users"}, {"media_library/audio/../../outside"}, {"media_library/audio\\other"}} {
		if err := ValidateCategories(dirs); err == nil {
			t.Fatalf("accepted %v", dirs)
		}
	}
	if err := ValidateCategories([]string{}); err != nil {
		t.Fatal(err)
	}
}
