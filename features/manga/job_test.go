package mangajob

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omnihub/internal/manga"
	"omnihub/internal/tasks"
)

func jpegFixture(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := jpeg.Encode(&buffer, picture, nil); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func temporaryArchive(t *testing.T, name string, cover []byte) {
	t.Helper()
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"metadata.json", []byte(`{"title":"fixture"}`)}, {"cover.jpg", cover}, {"chapter/2.jpg", jpegFixture(t)}} {
		out, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = out.Write(entry.data); err != nil {
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

func TestPublishValidatesAndNeverReplaces(t *testing.T) {
	t.Chdir(t.TempDir())
	temporaryArchive(t, ".omnihub-pack-good.tmp", jpegFixture(t))
	if err := Publish(".omnihub-pack-good.tmp", "book.cbz"); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile("book.cbz")
	if err != nil {
		t.Fatal(err)
	}
	temporaryArchive(t, ".omnihub-pack-second.tmp", jpegFixture(t))
	if err = Publish(".omnihub-pack-second.tmp", "book.cbz"); err == nil {
		t.Fatal("existing archive replaced")
	}
	current, err := os.ReadFile("book.cbz")
	if err != nil || sha256.Sum256(current) != sha256.Sum256(original) {
		t.Fatal("original archive changed")
	}
	temporaryArchive(t, ".omnihub-pack-invalid.tmp", []byte("broken"))
	for _, input := range [][2]string{{".omnihub-pack-invalid.tmp", "invalid.cbz"}, {"../outside.tmp", "escape.cbz"}, {".omnihub-pack-second.tmp", "../escape.cbz"}, {".omnihub-pack-second.tmp", ".hidden.cbz"}} {
		if err = Publish(input[0], input[1]); err == nil {
			t.Fatal("unsafe publication accepted")
		}
	}
	if _, err = os.Stat("invalid.cbz"); !os.IsNotExist(err) {
		t.Fatal("invalid cover published")
	}
}

func TestOfflinePackerInActualProcess(t *testing.T) {
	executable := os.Getenv("OMNIHUB_PACK_CHECK")
	if executable == "" {
		t.Skip("optional actual service-binary/uv packer check")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("existing uv required")
	}
	category, cache := t.TempDir(), t.TempDir()
	root := filepath.Join(category, "作品 Book")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	picture := jpegFixture(t)
	for _, name := range []string{"cover.jpg", "chapter/2.jpg", "chapter/10.jpg"} {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, picture, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "json.py"), []byte("raise RuntimeError(\"asset import executed\")\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(arguments ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, arguments...)
		command.Dir = root
		command.Env = append(os.Environ(), "UV_CACHE_DIR="+cache, "OMNIHUB_TASK_CATEGORY_DIR="+category)
		result, err := command.CombinedOutput()
		t.Log(string(result))
		return err
	}
	if err := run("--manga-pack", "book.cbz"); err != nil {
		t.Fatal(err)
	}
	reader, err := manga.OpenCBZ(filepath.Join(root, "book.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	pages, total, err := reader.PageWindow(0, 10)
	if err != nil || total != 2 || pages[0].Entry != "chapter/2.jpg" || pages[1].Entry != "chapter/10.jpg" {
		t.Fatal("natural-order reader incompatibility")
	}
	for _, name := range []string{"cover.jpg", "chapter/2.jpg", "chapter/10.jpg"} {
		archived, err := reader.ReadEntry(name)
		if err != nil || !bytes.Equal(archived, picture) {
			t.Fatal("source bytes changed")
		}
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(source, picture) {
			t.Fatal("original source changed")
		}
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	index, err := manga.OpenDBReadOnly(category)
	if err != nil {
		t.Fatal(err)
	}
	books, err := index.ListMangasPage(10, 0)
	if err != nil || len(books) != 1 || books[0].RelPath != "作品 Book/book.cbz" || books[0].Title != "作品 Book" {
		t.Fatalf("published book missing from index: %+v %v", books, err)
	}
	if err = index.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(category, "manga.db")); err != nil {
		t.Fatal(err)
	}
	if err = run("--manga-index"); err != nil {
		t.Fatal(err)
	}
	index, err = manga.OpenDBReadOnly(category)
	if err != nil {
		t.Fatal(err)
	}
	books, err = index.ListMangasPage(10, 0)
	if err != nil || len(books) != 1 || books[0].RelPath != "作品 Book/book.cbz" {
		t.Fatal("recursive index did not recover book")
	}
	if err = index.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "book.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	if err = run("--manga-pack", "book.cbz"); err == nil {
		t.Fatal("existing package overwritten")
	}
	after, err := os.ReadFile(filepath.Join(root, "book.cbz"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("previous package changed")
	}
	if err = os.WriteFile(filepath.Join(root, "cover.jpg"), []byte("invalid cover"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = run("--manga-pack", "bad-cover.cbz"); err == nil {
		t.Fatal("invalid cover accepted")
	}
	for _, pattern := range []string{".omnihub-pack-*.tmp", "bad-cover.cbz"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil || len(matches) != 0 {
			t.Fatalf("failed job left output: %v", matches)
		}
	}
}

func TestActualPackCancellationKeepsOriginals(t *testing.T) {
	executable := os.Getenv("OMNIHUB_PACK_CHECK")
	if executable == "" {
		t.Skip("optional actual managed packer cancellation")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("existing uv required")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cover.jpg"), jpegFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Create(filepath.Join(root, "page.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Write(jpegFixture(t)); err != nil {
		t.Fatal(err)
	}
	if err = source.Truncate(32 << 20); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	manager := tasks.NewManager()
	defer manager.Close()
	task, err := manager.RunCommand("cancel-pack", tasks.Command{Name: "pack", Executable: executable, Args: []string{"--manga-pack", "cancel.cbz"}, Directory: root, Resource: root, Write: true, Environment: []string{"UV_CACHE_DIR=" + t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	channel, history := task.SubscribeLogs(ctx)
	stopping := false
	for _, entry := range history {
		if strings.Contains(entry.Message, "INFO packing") {
			stopping = true
		}
	}
	if stopping {
		if err = manager.StopTask(task.ID); err != nil {
			t.Fatal(err)
		}
	}
	for entry := range channel {
		if !stopping && strings.Contains(entry.Message, "INFO packing") {
			stopping = true
			if err = manager.StopTask(task.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ctx.Err() != nil || !stopping {
		t.Fatal("managed packer never reached cancellable work")
	}
	matches, err := filepath.Glob(filepath.Join(root, ".omnihub-pack-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatal("normal cancellation left temporary files")
	}
	if _, err = os.Stat(filepath.Join(root, "cancel.cbz")); !os.IsNotExist(err) {
		t.Fatal("cancelled package was published")
	}
	info, err := os.Stat(filepath.Join(root, "page.jpg"))
	if err != nil || info.Size() != 32<<20 {
		t.Fatal("original page changed")
	}
}
