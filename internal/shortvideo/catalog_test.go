package shortvideo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVideoCatalogReadsOnlyChangedDirectories(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "creator")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(child, "first.mp4")
	if err := os.WriteFile(first, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	cache := NewVideoCatalog()
	now := time.Now()
	cache.clock = func() time.Time { return now }
	reads := 0
	cache.readDir = func(path string) ([]os.DirEntry, error) {
		reads++
		return readBoundedDirectory(path)
	}
	for i := 0; i < 12; i++ {
		path, err := cache.Random(context.Background(), root)
		if err != nil || path != filepath.Join("creator", "first.mp4") {
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
	if err != nil || path != filepath.Join("creator", "second.mp4") || reads != 3 {
		t.Fatalf("incremental update: %q, %v, reads=%d", path, err, reads)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Random(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestVideoCatalogIgnoresLinksAndDoesNotCommitFailedRefresh(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden.mp4"), []byte("hidden"), 0644); err != nil {
		t.Fatal(err)
	}
	cache := NewVideoCatalog()
	if _, err := cache.Random(context.Background(), root); !errors.Is(err, ErrNoVideos) {
		t.Fatalf("indexed hidden files or links: %v", err)
	}
	cache.Invalidate(root)
	cache.readDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("disk disconnected") }
	if _, err := cache.Random(context.Background(), root); err == nil {
		t.Fatal("scan error swallowed")
	}
	if len(cache.channels) != 0 {
		t.Fatal("partial refresh committed")
	}
}
