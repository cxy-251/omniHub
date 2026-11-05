package phoneops

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type localRemote struct {
	cut  bool
	sent int64
	free bool
}

func (r *localRemote) Shell(ctx context.Context, s string) ([]byte, error) {
	if r.free && strings.Contains(s, "stat -f") {
		return []byte("0 4096"), nil
	}
	return exec.CommandContext(ctx, "sh", "-c", s).CombinedOutput()
}
func (r *localRemote) Append(ctx context.Context, s string, in io.Reader) error {
	if r.cut {
		r.cut = false
		cmd := exec.CommandContext(ctx, "sh", "-c", s)
		cmd.Stdin = io.LimitReader(in, 12345)
		if err := cmd.Run(); err != nil {
			return err
		}
		r.sent += 12345
		return errors.New("simulated disconnect")
	}
	b, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	r.sent += int64(len(b))
	cmd := exec.CommandContext(ctx, "sh", "-c", s)
	cmd.Stdin = bytes.NewReader(b)
	return cmd.Run()
}
func fixture(t *testing.T) (*MangaPlan, *localRemote, []byte) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "phone")
	os.Mkdir(src, 0700)
	os.Mkdir(dst, 0700)
	data := bytes.Repeat([]byte("漫画\x00data"), 3000)
	os.WriteFile(filepath.Join(src, "中文 '漫画.cbz"), data, 0600)
	files, err := Catalogue(src)
	if err != nil {
		t.Fatal(err)
	}
	return &MangaPlan{ID: strings.Repeat("a", 32), Source: src, Target: dst, Files: files}, &localRemote{}, data
}
func TestMangaByteResumeAndRecycle(t *testing.T) {
	p, r, want := fixture(t)
	ctx := context.Background()
	save := func(*MangaPlan) error { return nil }
	r.cut = true
	if err := RunManga(ctx, r, p, save, nil, nil); err == nil {
		t.Fatal("interruption not returned")
	}
	if _, err := os.Stat(destination(p, p.Files[0])); !os.IsNotExist(err) {
		t.Fatal("partial published")
	}
	if err := RunManga(ctx, r, p, save, nil, nil); err != nil {
		t.Fatal(err)
	}
	if r.sent != int64(len(want)) {
		t.Fatalf("resume retransmitted: %d", r.sent)
	}
	got, _ := os.ReadFile(destination(p, p.Files[0]))
	if !bytes.Equal(got, want) {
		t.Fatal("bytes differ")
	}
	if !p.Files[0].Complete {
		t.Fatal("not journaled")
	}
	p.RemoveSource = true
	calls := 0
	if err := RunManga(ctx, r, p, save, nil, func(s string) error { calls++; return os.Rename(s, filepath.Join(t.TempDir(), "recycled.cbz")) }); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !p.Files[0].Recycled {
		t.Fatal("recycle not completed")
	}
	// Simulate a lost final checkpoint, after the OS trash already succeeded.
	p.Files[0].Recycled = false
	if err := RunManga(ctx, r, p, save, nil, func(string) error { t.Fatal("recycled twice"); return nil }); err != nil {
		t.Fatal(err)
	}
	if !p.Files[0].Recycled {
		t.Fatal("missing-source checkpoint was not recovered")
	}
}
func TestMangaGuards(t *testing.T) {
	for _, kind := range []string{"space", "conflict", "symlink", "changed", "bad-prefix", "checkpoint"} {
		t.Run(kind, func(t *testing.T) {
			p, r, data := fixture(t)
			ctx := context.Background()
			save := func(*MangaPlan) error { return nil }
			p.RemoveSource = true
			recycled := false
			switch kind {
			case "space":
				r.free = true
				p.Reserve = 1
			case "conflict":
				os.MkdirAll(filepath.Dir(destination(p, p.Files[0])), 0700)
				os.WriteFile(destination(p, p.Files[0]), bytes.Repeat([]byte("x"), len(data)), 0600)
			case "symlink":
				outside := t.TempDir()
				os.Symlink(outside, filepath.Join(p.Target, "media_library"))
			case "changed":
				os.WriteFile(filepath.Join(p.Source, p.Files[0].Rel), []byte("changed"), 0600)
			case "bad-prefix":
				os.MkdirAll(filepath.Dir(partial(p, p.Files[0])), 0700)
				os.WriteFile(partial(p, p.Files[0]), []byte("broken"), 0600)
			case "checkpoint":
				save = func(*MangaPlan) error { return errors.New("disk full") }
			}
			err := RunManga(ctx, r, p, save, nil, func(string) error { recycled = true; return nil })
			if err == nil {
				t.Fatal("guard accepted")
			}
			if recycled {
				t.Fatal("source recycled after failure")
			}
			if _, err = os.Stat(filepath.Join(p.Source, p.Files[0].Rel)); err != nil {
				t.Fatal("source missing")
			}
		})
	}
}
func TestCatalogueAndUSB(t *testing.T) {
	p, _, _ := fixture(t)
	os.Symlink(filepath.Join(p.Source, p.Files[0].Rel), filepath.Join(p.Source, "link.cbz"))
	if _, err := Catalogue(p.Source); err == nil {
		t.Fatal("symlink accepted")
	}
	if validRel("../book.cbz") || validRel("a\nbook.cbz") {
		t.Fatal("unsafe path accepted")
	}
	// A wireless transport to the correct phone must still be rejected.
	adb := filepath.Join(t.TempDir(), "adb")
	os.WriteFile(adb, []byte("#!/bin/sh\necho unknown\n"), 0700)
	if (ADB{Path: adb, Serial: "phone"}).RequireUSB(context.Background()) == nil {
		t.Fatal("wireless accepted")
	}
	os.WriteFile(adb, []byte("#!/bin/sh\necho usb:1-2\n"), 0700)
	if err := (ADB{Path: adb, Serial: "phone"}).RequireUSB(context.Background()); err != nil {
		t.Fatal(err)
	}
}
