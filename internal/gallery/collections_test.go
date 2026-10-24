package gallery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"omnihub/internal/storage"
)

func pngBytes(t *testing.T, w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeVolume(t *testing.T, path string, blobs ...[]byte) {
	db, err := storage.OpenSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE originals (id TEXT PRIMARY KEY, data BLOB)"); err != nil {
		t.Fatal(err)
	}
	for _, b := range blobs {
		sum := sha256.Sum256(b)
		if _, err := db.Exec("INSERT INTO originals (id, data) VALUES (?, ?)", hex.EncodeToString(sum[:]), b); err != nil {
			t.Fatal(err)
		}
	}
}

// 只放了原图分卷的图库：建索引后可浏览；分卷挪进 originals/；再加一卷时提示要补索引，补完只加新的。
func TestIndexVolumesCollection(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Twice")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	red, blue := pngBytes(t, 40, 20, color.RGBA{220, 20, 20, 255}), pngBytes(t, 10, 30, color.RGBA{20, 20, 220, 255})
	writeVolume(t, filepath.Join(dir, "originals_part_001.db"), red, blue, []byte("not an image"))
	cs := Collections(root)
	if len(cs) != 1 || cs[0].Key != "Twice" || cs[0].Indexed || !cs[0].NeedsIndex {
		t.Fatalf("before index: %+v", cs)
	}
	res, err := IndexVolumes(context.Background(), dir, nil)
	if err != nil || res.Added != 2 || res.Unsupported != 1 || len(res.Errors) != 0 {
		t.Fatalf("index: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "originals", "originals_part_001.db")); err != nil {
		t.Fatal("volume not moved into originals/")
	}
	if cs = Collections(root); !cs[0].Indexed || cs[0].NeedsIndex {
		t.Fatalf("after index: %+v", cs)
	}
	meta, err := OpenMetaDBReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := meta.CountPhotos(PhotoFilter{Color: -1}); n != 2 {
		t.Fatalf("photos = %d", n)
	}
	sum := sha256.Sum256(red)
	photo, _ := meta.GetPhoto(hex.EncodeToString(sum[:]))
	meta.Close()
	if photo == nil || photo.Width != 40 || photo.Height != 20 || photo.Volume != "originals_part_001.db" {
		t.Fatalf("photo = %+v", photo)
	}
	if data, err := ReadOriginalDirect(filepath.Join(dir, "originals"), photo.Volume, photo.ID); err != nil || !bytes.Equal(data, red) {
		t.Fatalf("read original: %v", err)
	}
	green := pngBytes(t, 8, 8, color.RGBA{20, 200, 20, 255})
	writeVolume(t, filepath.Join(dir, "originals", "originals_part_002.db"), green, red)
	if cs = Collections(root); !cs[0].NeedsIndex {
		t.Fatalf("new volume not noticed: %+v", cs)
	}
	if res, err = IndexVolumes(context.Background(), dir, nil); err != nil || res.Added != 1 || res.Known != 3 {
		t.Fatalf("reindex: %+v %v", res, err)
	}
}
