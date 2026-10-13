package manga

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestReadingPageWindowExcludesCoverAndRoundTripsLegacyNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pages.cbz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	legacy := string([]byte{0x81, 0x82}) + ".jpg"
	for _, name := range []string{"cover.jpg", "01/10.jpg", "01/2.jpg", "02/1.jpg", legacy} {
		entry, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("image")); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := OpenCBZ(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pages, total, err := r.PageWindow(0, 2)
	if err != nil || total != 4 || len(pages) != 2 || pages[0].Entry != "01/2.jpg" || pages[1].Entry != "01/10.jpg" {
		t.Fatalf("wrong reading pages: %+v %d %v", pages, total, err)
	}
	pages[0].Entry = "mutated"
	fresh, _, err := r.PageWindow(0, 1)
	if err != nil || fresh[0].Entry != "01/2.jpg" {
		t.Fatal("caller changed cached page directory")
	}
	key := EncodeEntry(legacy)
	decoded, err := DecodeEntry(key)
	if err != nil || decoded != legacy {
		t.Fatal("legacy filename bytes lost")
	}
	data, err := r.ReadEntry(decoded)
	if err != nil || string(data) != "image" {
		t.Fatal("opaque page key cannot read original bytes")
	}
	if _, _, err := r.PageWindow(0, 101); err == nil {
		t.Fatal("unbounded page window accepted")
	}
}
