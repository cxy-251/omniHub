package archiveutil

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func container(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.Create("entry.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(bytes.Repeat([]byte("x"), 4096)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func writeContainer(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.zip")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEntryBudgetAndCancellation(t *testing.T) {
	r, err := Open(writeContainer(t, container(t)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := ReadEntry(context.Background(), r.File[0], 1024); !errors.Is(err, ErrCapacity) {
		t.Fatalf("oversized entry accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadEntry(ctx, r.File[0], 8192); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data, err := ReadEntry(context.Background(), r.File[0], 8192)
	if err != nil || len(data) != 4096 {
		t.Fatalf("normal read: %d %v", len(data), err)
	}
}

func TestRejectUntrustedDirectoryBudgets(t *testing.T) {
	for _, kind := range []string{"count", "size", "actual-count"} {
		t.Run(kind, func(t *testing.T) {
			data := container(t)
			end := data[len(data)-22:]
			switch kind {
			case "count":
				binary.LittleEndian.PutUint16(end[8:], maxEntries+1)
				binary.LittleEndian.PutUint16(end[10:], maxEntries+1)
			case "size":
				binary.LittleEndian.PutUint32(end[12:], maxDirectoryBytes+1)
			case "actual-count":
				binary.LittleEndian.PutUint16(end[8:], 0)
				binary.LittleEndian.PutUint16(end[10:], 0)
			}
			if r, err := Open(writeContainer(t, data)); err == nil {
				r.Close()
				t.Fatal("malformed directory accepted")
			}
		})
	}
}

func TestZIP64Directory(t *testing.T) {
	data := container(t)
	position := len(data) - 22
	end := append([]byte(nil), data[position:]...)
	var record [56]byte
	binary.LittleEndian.PutUint32(record[:], 0x06064b50)
	binary.LittleEndian.PutUint64(record[4:], 44)
	binary.LittleEndian.PutUint16(record[12:], 45)
	binary.LittleEndian.PutUint16(record[14:], 45)
	binary.LittleEndian.PutUint64(record[24:], 1)
	binary.LittleEndian.PutUint64(record[32:], 1)
	binary.LittleEndian.PutUint64(record[40:], uint64(binary.LittleEndian.Uint32(end[12:])))
	binary.LittleEndian.PutUint64(record[48:], uint64(binary.LittleEndian.Uint32(end[16:])))
	var locator [20]byte
	binary.LittleEndian.PutUint32(locator[:], 0x07064b50)
	binary.LittleEndian.PutUint64(locator[8:], uint64(position))
	binary.LittleEndian.PutUint32(locator[16:], 1)
	binary.LittleEndian.PutUint16(end[8:], 0xffff)
	binary.LittleEndian.PutUint16(end[10:], 0xffff)
	binary.LittleEndian.PutUint32(end[12:], 0xffffffff)
	binary.LittleEndian.PutUint32(end[16:], 0xffffffff)
	data = append(data[:position], record[:]...)
	data = append(data, locator[:]...)
	data = append(data, end...)
	r, err := Open(writeContainer(t, data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	content, err := ReadEntry(context.Background(), r.File[0], 8192)
	if err != nil || len(content) != 4096 {
		t.Fatalf("ZIP64 read: %d %v", len(content), err)
	}

	// Move only the directory beyond 4 GiB in a sparse file. This validates
	// large ZIP64 offsets without allocating or writing a multi-gigabyte fixture.
	oldOffset := binary.LittleEndian.Uint64(record[48:])
	newOffset := int64(1<<32 + 128)
	newPosition := newOffset + int64(binary.LittleEndian.Uint64(record[40:]))
	binary.LittleEndian.PutUint64(record[48:], uint64(newOffset))
	binary.LittleEndian.PutUint64(locator[8:], uint64(newPosition))
	path := filepath.Join(t.TempDir(), "large.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []struct {
		data   []byte
		offset int64
	}{
		{data[:oldOffset], 0},
		{data[oldOffset:position], newOffset},
		{record[:], newPosition},
		{locator[:], newPosition + 56},
		{end, newPosition + 76},
	} {
		if _, err := f.WriteAt(part.data, part.offset); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	large, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer large.Close()
	content, err = ReadEntry(context.Background(), large.File[0], 8192)
	if err != nil || len(content) != 4096 {
		t.Fatalf("large offset read: %d %v", len(content), err)
	}
}

func TestDuplicateNamesRejected(t *testing.T) {
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for range 2 {
		entry, err := w.Create("same.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("content")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if r, err := Open(writeContainer(t, data.Bytes())); err == nil {
		r.Close()
		t.Fatal("ambiguous entry names accepted")
	}
}
