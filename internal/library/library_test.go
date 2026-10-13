package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMountSnapshotsAndExplicitPrimaryRemoval(t *testing.T) {
	mgr := NewManager()
	first, err := mgr.AddLibrary(t.TempDir(), false)
	if err != nil || !first.IsPrimary {
		t.Fatalf("first mount must be primary: %v", err)
	}
	first.RootPath = "modified by caller"
	if mgr.GetPrimary().RootPath == first.RootPath {
		t.Fatal("mount exposed mutable manager state")
	}
	second, err := mgr.AddLibrary(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, lib := range mgr.ListLibraries() {
		if lib.IsPrimary != (lib.ID == second.ID) {
			t.Fatal("inconsistent primary flags")
		}
		lib.IsPrimary = false
	}
	if !mgr.GetPrimary().IsPrimary {
		t.Fatal("list exposed mutable manager state")
	}
	primary := mgr.GetPrimary()
	primary.Name = "modified by caller"
	if mgr.GetPrimary().Name == primary.Name {
		t.Fatal("primary query exposed mutable manager state")
	}
	mgr.RemoveLibrary(second.ID)
	if mgr.GetPrimary() != nil || len(mgr.ListLibraries()) != 1 {
		t.Fatal("removal silently selected a new primary")
	}
}

func TestMountRejectsDuplicateIdentityAndFileRoot(t *testing.T) {
	mgr := NewManager()
	root := t.TempDir()
	first, err := mgr.AddLibrary(root, true)
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	marker, err := os.ReadFile(filepath.Join(root, LibraryMarkerFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, LibraryMarkerFile), marker, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.MountLibrary(other, true, true); err == nil {
		t.Fatal("duplicate library replaced existing mount")
	}
	if mgr.GetPrimary().ID != first.ID || mgr.GetPrimary().RootPath != root {
		t.Fatal("failed mount changed primary")
	}
	if _, err := mgr.AddLibrary(filepath.Join(root, LibraryMarkerFile), false); err == nil {
		t.Fatal("accepted file as library root")
	}
}

func TestLibraryZeroFaultTolerance(t *testing.T) {
	mgr := NewManager()
	if mgr.GetPrimary() != nil {
		t.Error("expected nil primary library for empty manager")
	}
	if len(mgr.ListLibraries()) != 0 {
		t.Errorf("expected 0 libraries, got %d", len(mgr.ListLibraries()))
	}
}

func TestLibraryAddAndResolvePath(t *testing.T) {
	tempDir := t.TempDir()

	mgr := NewManager()
	lib, err := mgr.AddLibrary(tempDir, true)
	if err != nil {
		t.Fatalf("failed to add library: %v", err)
	}

	if !lib.IsPrimary {
		t.Error("expected primary library")
	}
	if !lib.IsOnline {
		t.Error("expected online library")
	}

	// 检查标识文件生成
	markerPath := filepath.Join(tempDir, LibraryMarkerFile)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Errorf("marker file not created at %s", markerPath)
	}

	// 检查主库 .omni_data 目录生成
	dataDir := filepath.Join(tempDir, DataDirName)
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Errorf("data dir not created at %s", dataDir)
	}

	// 路径解析验证
	logicalPath := lib.ID + ":media_library/manga/test.cbz"
	resolved, err := mgr.ResolvePath(logicalPath)
	if err != nil {
		t.Fatalf("failed to resolve path: %v", err)
	}

	expected := filepath.Join(tempDir, "media_library", "manga", "test.cbz")
	if resolved != expected {
		t.Errorf("expected resolved path %s, got %s", expected, resolved)
	}
}

func TestLibrarySoftDisableCategory(t *testing.T) {
	mgr := NewManager()
	cat := "media_library/manga"

	if mgr.IsCategoryDisabled(cat) {
		t.Error("category should not be disabled by default")
	}

	mgr.SetCategoryDisabled(cat, true)
	if !mgr.IsCategoryDisabled(cat) {
		t.Error("category should be disabled")
	}

	mgr.SetCategoryDisabled(cat, false)
	if mgr.IsCategoryDisabled(cat) {
		t.Error("category should be re-enabled")
	}
}

func TestEnsureMarkerPreservesInvalidIdentity(t *testing.T) {
	for _, contents := range []string{"broken json", `{"id":""}`} {
		t.Run(contents, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, LibraryMarkerFile)
			if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := EnsureMarker(root, ""); err == nil {
				t.Fatal("expected invalid marker error")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != contents {
				t.Fatalf("marker was modified: %q, %v", got, err)
			}
		})
	}
}

func TestResolvePathRejectsLibraryEscape(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager()
	lib, err := mgr.AddLibrary(root, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", "a/../../secret", "/absolute", `..\secret`, "C:/secret", "", "a\x00b"} {
		if _, err := mgr.ResolvePath(lib.ID + ":" + path); err == nil {
			t.Errorf("accepted invalid path %q", path)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ResolvePath("escape/not-created/file"); err == nil {
		t.Fatal("accepted escaping symlink for nonexistent file")
	}
	if err := os.Symlink(filepath.Join(root, "media_library"), filepath.Join(root, "inside")); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ResolvePath("inside/manga/new.cbz"); err != nil {
		t.Fatalf("rejected internal symlink: %v", err)
	}
}

func TestCategoryPathRejectsOtherSectionsAndAliases(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager()
	if _, err := mgr.AddLibrary(root, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, DataDirName), filepath.Join(root, "media_library", "audio", "private")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".omni_data/users.db", "media_library/audio/private/users.db", "media_library/docs/readme.md"} {
		if _, err := mgr.ResolveCategoryPath(path, "media_library/audio"); err == nil {
			t.Errorf("accepted cross-category resource %q", path)
		}
	}
	if _, err := mgr.ResolveCategoryPath("media_library/audio/album/track.mp3", "media_library/audio"); err != nil {
		t.Fatal(err)
	}
	mgr.SetCategoryDisabled("media_library/audio", true)
	if _, err := mgr.ResolveCategoryPath("media_library/audio/track.mp3", "media_library/audio"); err == nil {
		t.Fatal("disabled category still readable")
	}
}

func TestReadOnlyMountDoesNotCreateSkeleton(t *testing.T) {
	root := t.TempDir()
	marker := []byte(`{"id":"existing-library","name":"Read only"}`)
	if err := os.WriteFile(filepath.Join(root, LibraryMarkerFile), marker, 0644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager()
	lib, err := mgr.MountLibrary(root, true, true)
	if err != nil || !lib.ReadOnly {
		t.Fatalf("mount: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("read-only mount wrote to library: %v, %v", entries, err)
	}
	missing := t.TempDir()
	if _, err := mgr.MountLibrary(missing, false, true); err == nil {
		t.Fatal("read-only mount created missing marker")
	}
	entries, err = os.ReadDir(missing)
	if err != nil || len(entries) != 0 {
		t.Fatal("missing-marker library was modified")
	}
}
