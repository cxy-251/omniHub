package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogReadsOnlyChangedDirectories(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "creator")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(child, "first.mp4")
	if err := os.WriteFile(first, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	cache := New(".mp4")
	now := time.Now()
	cache.clock = func() time.Time { return now }
	reads := 0
	cache.readDir = func(root *os.Root, path string) ([]os.DirEntry, error) {
		reads++
		return readBoundedDirectory(root, path)
	}
	for i := 0; i < 12; i++ {
		path, err := cache.Random(context.Background(), root)
		if err != nil || path != "creator/first.mp4" {
			t.Fatalf("random path: %q, %v", path, err)
		}
	}
	if reads != 2 {
		t.Fatalf("matrix requests rescanned directories: %d reads", reads)
	}
	now = now.Add(11 * time.Second)
	if _, err := cache.Random(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatal("unchanged directory reread")
	}
	if err := os.Rename(first, filepath.Join(child, "second.mp4")); err != nil {
		t.Fatal(err)
	}
	// 显式改变目录时间戳，避免低精度文件系统让测试依赖等待。
	if err := os.Chtimes(child, now, now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	path, err := cache.Random(context.Background(), root)
	if err != nil || path != "creator/second.mp4" || reads != 3 {
		t.Fatalf("incremental update: %q, %v, reads=%d", path, err, reads)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Random(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestCatalogIgnoresLinksAndDoesNotCommitFailedRefresh(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden.mp4"), []byte("hidden"), 0644); err != nil {
		t.Fatal(err)
	}
	cache := New(".mp4")
	if _, err := cache.Random(context.Background(), root); !errors.Is(err, ErrNoFiles) {
		t.Fatalf("indexed hidden files or links: %v", err)
	}
	cache.Invalidate(root)
	cache.readDir = func(*os.Root, string) ([]os.DirEntry, error) { return nil, errors.New("disk disconnected") }
	if _, err := cache.Random(context.Background(), root); err == nil {
		t.Fatal("scan error swallowed")
	}
	if len(cache.roots) != 0 {
		t.Fatal("partial refresh committed")
	}
}

func TestCatalogPageCopiesAndRefreshesReplacedDirectory(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "library")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b.txt", "a.txt", "ignore.mp4"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cache := New(".txt")
	now := time.Now()
	cache.clock = func() time.Time { return now }
	page, total, err := cache.Page(context.Background(), root, 1, 0)
	if err != nil || total != 2 || len(page) != 1 || page[0] != "a.txt" {
		t.Fatalf("page: %v %d %v", page, total, err)
	}
	page[0] = "caller mutation"
	page, _, err = cache.Page(context.Background(), root, 1, 0)
	if err != nil || page[0] != "a.txt" {
		t.Fatal("caller mutated cached paths")
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, filepath.Join(base, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "replacement.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(root, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	page, total, err = cache.Page(context.Background(), root, 100, 0)
	if err != nil || total != 1 || page[0] != "replacement.txt" {
		t.Fatalf("replaced directory not detected: %v %d %v", page, total, err)
	}
}
