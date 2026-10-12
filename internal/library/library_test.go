package library

import (
	"os"
	"path/filepath"
	"testing"
)

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
