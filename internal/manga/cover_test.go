package manga

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestCoverFallbackUsesOnlyFirstThreePagesOfFirstChapter(t *testing.T) {
	var valid bytes.Buffer
	if err := png.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	// This has a valid PNG header but no complete image payload: config-only
	// checks would wrongly accept it instead of reaching the third page.
	truncated := valid.Bytes()[:33]
	for _, test := range []struct {
		name    string
		entries map[string][]byte
		custom  string
		want    bool
	}{
		{"third-page", map[string][]byte{"cover.png": []byte("broken"), "01/1.png": truncated, "01/2.png": []byte("broken"), "01/3.png": valid.Bytes()}, "", true},
		{"fourth-page-excluded", map[string][]byte{"01/1.png": truncated, "01/2.png": truncated, "01/3.png": truncated, "01/4.png": valid.Bytes()}, "", false},
		{"next-chapter-excluded", map[string][]byte{"01/1.png": truncated, "02/1.png": valid.Bytes()}, "", false},
		{"custom-later-page", map[string][]byte{"01/1.png": truncated, "02/1.png": valid.Bytes()}, "02/1.png", true},
		{"broken-custom-falls-back", map[string][]byte{"cover.png": valid.Bytes(), "01/1.png": truncated}, "01/1.png", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := os.Create(filepath.Join(t.TempDir(), "book.cbz"))
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(file)
			for name, data := range test.entries {
				entry, err := z.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := errors.Join(z.Close(), file.Close()); err != nil {
				t.Fatal(err)
			}
			r, err := OpenCBZ(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := r.Close(); err != nil {
					t.Error(err)
				}
			})
			for i := 0; i < 2; i++ {
				data, err := r.ReadCoverContext(context.Background(), test.custom)
				if test.want && (err != nil || !bytes.Equal(data, valid.Bytes())) {
					t.Fatalf("expected usable cover: %v", err)
				}
				if !test.want && err == nil {
					t.Fatal("selected a page outside the fallback budget")
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := r.ReadCoverContext(ctx, test.custom); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled cached/uncached request: %v", err)
			}
		})
	}

	oversize := append([]byte(nil), valid.Bytes()...)
	binary.BigEndian.PutUint32(oversize[16:20], maxCoverPixels)
	binary.BigEndian.PutUint32(oversize[20:24], 2)
	binary.BigEndian.PutUint32(oversize[29:33], crc32.ChecksumIEEE(oversize[12:29]))
	if err := validateCover(context.Background(), oversize); err == nil || err.Error() != "cover image exceeds pixel capacity" {
		t.Fatalf("pixel budget failed before raster allocation: %v", err)
	}
}
