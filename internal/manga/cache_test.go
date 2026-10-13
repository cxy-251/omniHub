package manga

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeCacheBook(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, err := w.Create("1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReaderCacheReusesDirectoryAndRetiresReplacedFileSafely(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "book.cbz")
	writeCacheBook(t, path, "old")
	cache := NewReaderCache()
	defer cache.Close()
	opens := 0
	cache.open = func(path string) (*CBZReader, error) { opens++; return OpenCBZ(path) }
	for range 12 {
		lease, err := cache.Acquire(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := lease.Reader.ReadEntry("1.jpg")
		if err != nil || string(data) != "old" {
			t.Fatal("cached read failed")
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := lease.Reader.zr.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("idle cache retained an open file")
		}
	}
	if opens != 1 {
		t.Fatalf("directory reopened for repeated requests: %d", opens)
	}
	old, err := cache.Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "replacement.cbz")
	writeCacheBook(t, replacement, "new")
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	fresh, err := cache.Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := fresh.Reader.ReadEntry("1.jpg")
	if err != nil || string(data) != "new" {
		t.Fatal("replacement not detected")
	}
	data, err = old.Reader.ReadEntry("1.jpg")
	if err != nil || string(data) != "old" {
		t.Fatal("active reader closed by replacement")
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal("double release failed")
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	data, err = fresh.Reader.ReadEntry("1.jpg")
	if err != nil || string(data) != "new" {
		t.Fatal("shutdown closed active lease")
	}
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Acquire(context.Background(), path); err == nil {
		t.Fatal("closed cache accepted new lease")
	}
	if len(cache.all) != 0 {
		t.Fatal("read descriptors leaked")
	}
}

func TestReaderCacheDescriptorBudgetAndCanceledAcquire(t *testing.T) {
	root := t.TempDir()
	cache := NewReaderCache()
	defer cache.Close()
	var leases []*ReaderLease
	defer func() {
		for _, lease := range leases {
			if err := lease.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < maxOpenReaders+1; i++ {
		path := filepath.Join(root, string(rune('a'+i))+".cbz")
		writeCacheBook(t, path, "page")
		lease, err := cache.Acquire(context.Background(), path)
		if i == maxOpenReaders {
			if err == nil {
				lease.Close()
				t.Fatal("unbounded open readers")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			leases = append(leases, lease)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Acquire(ctx, filepath.Join(root, "a.cbz")); err == nil {
		t.Fatal("canceled request acquired reader")
	}
}
