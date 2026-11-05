package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTrashSharedAndPreviewContentType(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	var trashed string
	if err := TrashShared(dir, "hello.txt", func(p string) error {
		trashed = p
		return os.Remove(p)
	}); err != nil {
		t.Fatalf("TrashShared failed: %v", err)
	}
	if trashed != file {
		t.Fatalf("unexpected trashed path: %s", trashed)
	}
	if err := TrashShared(dir, "../outside.txt", func(p string) error { return nil }); err != ErrInvalidName {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
	if ct, ok := PreviewContentType("note.md"); !ok || ct != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected preview type for note.md: %s %v", ct, ok)
	}
	if _, ok := PreviewContentType("xss.html"); ok {
		t.Fatal("html should not be previewable")
	}
	if _, ok := PreviewContentType("xss.svg"); ok {
		t.Fatal("svg should not be previewable")
	}
}

func TestScanTopFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "small.bin"), make([]byte, 128), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "large.bin"), make([]byte, 3<<20), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "mid.bin"), make([]byte, 2<<20), 0o644)

	roots := []struct{ Name, Path string }{{Name: "主库", Path: dir}}
	top, err := ScanTopFiles(context.Background(), roots, 2)
	if err != nil {
		t.Fatalf("ScanTopFiles failed: %v", err)
	}
	if len(top) != 2 || top[0].RelPath != "large.bin" || top[1].RelPath != "mid.bin" {
		t.Fatalf("unexpected top files: %+v", top)
	}
}
