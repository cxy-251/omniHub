package manga

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func metadataFixture(t *testing.T, path, jsonData, xmlData string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entries := map[string]string{"a/1.jpg": "page", "a/2.jpg": "page", "b/1.jpg": "page"}
	if jsonData != "" {
		entries["metadata.json"] = jsonData
	}
	if xmlData != "" {
		entries["ComicInfo.xml"] = xmlData
	}
	for name, data := range entries {
		out, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = out.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestComicInfoMergeRespectsExplicitJSONAndActualChapters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	source := `<ComicInfo><Series>Series</Series><Writer>Writer</Writer><Summary>Plain &amp; safe</Summary><Genre>Drama, Adventure</Genre><Tags>Drama; Custom</Tags><Count>999</Count></ComicInfo>`
	metadataFixture(t, path, "", source)
	book, err := ScanMangaFromCBZ(path)
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "Series" || book.Author != "Writer" || book.Description != "Plain & safe" || len(book.Tags) != 3 || book.ChapterCount != 2 || book.IsComplete {
		t.Fatalf("XML fields or real chapter count incorrect: %+v", book)
	}
	metadataFixture(t, path, `{"Title":"JSON title","Author":"","description":null,"tags":[],"local_chapters":0,"is_complete":false}`, source)
	book, err = ScanMangaFromCBZ(path)
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "JSON title" || book.Author != "" || book.Description != "" || len(book.Tags) != 0 || book.ChapterCount != 0 || book.IsComplete {
		t.Fatalf("explicit JSON overwritten: %+v", book)
	}
}

func TestComicInfoRejectsUnsafeOrUnboundedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	for _, source := range []string{
		`<!DOCTYPE ComicInfo [<!ENTITY x SYSTEM "file:///not-read">]><ComicInfo><Title>&x;</Title></ComicInfo>`,
		`<ComicInfo><Title>one</Title><Title>two</Title></ComicInfo>`,
		`<ComicInfo><Title><b>markup</b></Title></ComicInfo>`,
		`<Other><Title>bad root</Title></Other>`,
		`<ComicInfo></ComicInfo><ComicInfo/>`,
		`<ComicInfo><Title>` + strings.Repeat("x", 4097) + `</Title></ComicInfo>`,
		`<ComicInfo>` + strings.Repeat(`<Unknown>`, 65) + strings.Repeat(`</Unknown>`, 65) + `</ComicInfo>`,
	} {
		metadataFixture(t, path, "", source)
		if _, err := ScanMangaFromCBZ(path); err == nil {
			t.Fatal("unsafe XML accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanMangaFromCBZContext(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}

func TestDiscoveryMetadataCopiesInvalidatesAndClosesArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	metadataFixture(t, path, `{"title":"first","tags":["original"]}`, "")
	cache := NewDiscoveryCache()
	defer cache.Close()
	book, err := cache.Metadata(context.Background(), path, "nested/book.cbz")
	if err != nil {
		t.Fatal(err)
	}
	book.Tags[0] = "mutated"
	again, err := cache.Metadata(context.Background(), path, "nested/book.cbz")
	if err != nil || again.Tags[0] != "original" || again.ID != PathIdentity("nested/book.cbz") {
		t.Fatal("cache alias or unstable identity")
	}
	moved := path + ".old"
	if err = os.Rename(path, moved); err != nil {
		t.Fatal("reader kept source descriptor open", err)
	}
	metadataFixture(t, path, `{"title":"replacement book"}`, "")
	again, err = cache.Metadata(context.Background(), path, "nested/book.cbz")
	if err != nil || again.Title != "replacement book" {
		t.Fatal("replaced file reused old metadata")
	}
}

func TestDiscoveryCacheRetainedBudget(t *testing.T) {
	root := t.TempDir()
	cache := NewDiscoveryCache()
	defer cache.Close()
	for index := 0; index < discoveryEntries+4; index++ {
		path := filepath.Join(root, fmt.Sprintf("book-%03d.cbz", index))
		metadataFixture(t, path, `{"title":"Book","description":"`+strings.Repeat("x", 32<<10)+`"}`, "")
		if _, err := cache.Metadata(context.Background(), path, filepath.Base(path)); err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for _, entry := range cache.entries {
		total += entry.bytes
	}
	if len(cache.entries) > discoveryEntries || total > discoveryBudget {
		t.Fatal("metadata cache exceeded retained budget")
	}
	cache.Close()
	if len(cache.entries) != 0 {
		t.Fatal("closed cache retained metadata")
	}
}
