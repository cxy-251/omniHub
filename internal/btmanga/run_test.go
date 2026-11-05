package btmanga

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"omnihub/internal/manga"
	"os"
	"path/filepath"
	"testing"
)

func TestPackPreservesSourcesAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source")
	if err := os.Mkdir(input, 0755); err != nil {
		t.Fatal(err)
	}
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Volume 10.cbz", "Volume 2.cbz"} {
		f, err := os.Create(filepath.Join(input, name))
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		w, err := z.Create("01.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(img.Bytes()); err != nil {
			t.Fatal(err)
		}
		if err = z.Close(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	dest := filepath.Join(dir, "Book.cbz")
	if err := Pack(context.Background(), input, dest, io.Discard); err != nil {
		t.Fatal(err)
	}
	r, err := manga.OpenCBZ(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ListImageEntries()) != 2 || len(r.Chapters()) != 2 {
		t.Fatal("volume entries were lost or merged")
	}
	r.Close()
	before, _ := os.ReadFile(dest)
	if err = Pack(context.Background(), input, dest, io.Discard); err == nil {
		t.Fatal("overwrote existing book")
	}
	after, _ := os.ReadFile(dest)
	if !bytes.Equal(before, after) {
		t.Fatal("existing book changed")
	}
	for _, name := range []string{"Volume 10.cbz", "Volume 2.cbz"} {
		if _, err = os.Stat(filepath.Join(input, name)); err != nil {
			t.Fatal("deleted source", err)
		}
	}
}

func TestPackRejectsBrokenPageAndLinksWithoutPublishing(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source")
	os.Mkdir(input, 0755)
	bad := filepath.Join(input, "01.jpg")
	os.WriteFile(bad, []byte("broken image"), 0644)
	dest := filepath.Join(dir, "Book.cbz")
	if err := Pack(context.Background(), input, dest, io.Discard); err == nil {
		t.Fatal("accepted a broken image")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("published invalid book")
	}
	if _, err := os.Stat(bad); err != nil {
		t.Fatal("removed input")
	}
	if err := os.Symlink(bad, filepath.Join(input, "02.jpg")); err == nil {
		if err = Pack(context.Background(), input, dest, io.Discard); err == nil {
			t.Fatal("accepted symlink")
		}
	}
}
