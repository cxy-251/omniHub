package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMetadataCacheCoalescesReadsAndReturnsIndependentPictures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewMetadataCache()
	defer c.Close()
	var parses atomic.Int32
	var opened *os.File
	c.parse = func(ctx context.Context, f *os.File) (Metadata, error) {
		parses.Add(1)
		opened = f
		return Metadata{Title: "title", HasCover: true, Picture: []byte("picture")}, nil
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			value, err := c.Get(context.Background(), path, false)
			if err != nil || value.Title != "title" || !value.HasCover || len(value.Picture) != 0 {
				t.Errorf("metadata hit: %+v %v", value, err)
			}
		})
	}
	wg.Wait()
	if parses.Load() != 1 {
		t.Fatalf("reparsed %d times", parses.Load())
	}
	if _, err := opened.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("idle cache kept descriptor: %v", err)
	}
	first, err := c.Get(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	first.Picture[0] = 'X'
	second, err := c.Get(context.Background(), path, true)
	if err != nil || string(second.Picture) != "picture" {
		t.Fatal("caller mutated cached picture")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(path), "replacement")
	if err := os.WriteFile(replacement, []byte("next"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), path, false); err != nil {
		t.Fatal(err)
	}
	if parses.Load() != 2 {
		t.Fatal("same-size/time replacement reused old labels")
	}
	c.Close()
	c.Close()
	if _, err := c.Get(context.Background(), path, false); !errors.Is(err, ErrMetadataCacheClosed) {
		t.Fatalf("closed read: %v", err)
	}
}

func TestMetadataCacheCancelsWaitingAndCannotRepopulateAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewMetadataCache()
	started, release := make(chan struct{}), make(chan struct{})
	c.parse = func(ctx context.Context, f *os.File) (Metadata, error) {
		close(started)
		<-release
		return Metadata{Title: "title"}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := c.Get(context.Background(), path, false); done <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Get(ctx, path, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	c.Close()
	close(release)
	if err := <-done; !errors.Is(err, ErrMetadataCacheClosed) {
		t.Fatalf("closed during parse: %v", err)
	}
	if len(c.entries) != 0 || c.bytes != 0 {
		t.Fatal("active parse repopulated closed cache")
	}
}

func TestMetadataCacheRetentionBudgets(t *testing.T) {
	dir := t.TempDir()
	c := NewMetadataCache()
	defer c.Close()
	c.parse = func(context.Context, *os.File) (Metadata, error) {
		return Metadata{HasCover: true, Picture: bytes.Repeat([]byte{'x'}, 2<<20)}, nil
	}
	for i := range 10 {
		path := filepath.Join(dir, fmt.Sprintf("%d.mp3", i))
		if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Get(context.Background(), path, false); err != nil {
			t.Fatal(err)
		}
		if c.bytes > metadataCacheBytes || len(c.entries) > metadataCacheEntries {
			t.Fatal("cache exceeded retention budget")
		}
	}
	if len(c.entries) != 3 {
		t.Fatalf("picture LRU did not evict: %d", len(c.entries))
	}
	c.Close()
	c = NewMetadataCache()
	defer c.Close()
	c.parse = func(context.Context, *os.File) (Metadata, error) { return Metadata{Title: "small"}, nil }
	for i := range 40 {
		path := filepath.Join(dir, fmt.Sprintf("small-%d.mp3", i))
		if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Get(context.Background(), path, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.entries) != metadataCacheEntries {
		t.Fatalf("entry count cap: %d", len(c.entries))
	}
	c.parse = func(context.Context, *os.File) (Metadata, error) {
		return Metadata{HasCover: true, Picture: bytes.Repeat([]byte{'x'}, metadataCacheItemBytes+1)}, nil
	}
	path := filepath.Join(dir, "large.mp3")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), path, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.entries[path]; ok {
		t.Fatal("oversized result retained")
	}
}

func TestMetadataCacheDoesNotRetainFilesChangedDuringParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewMetadataCache()
	defer c.Close()
	c.parse = func(context.Context, *os.File) (Metadata, error) {
		if err := os.WriteFile(path, []byte("changed size"), 0600); err != nil {
			return Metadata{}, err
		}
		return Metadata{Title: "obsolete"}, nil
	}
	if _, err := c.Get(context.Background(), path, false); err == nil {
		t.Fatal("changed file returned stale metadata")
	}
	if len(c.entries) != 0 || c.bytes != 0 {
		t.Fatal("changed file polluted cache")
	}
}
