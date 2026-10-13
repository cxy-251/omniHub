// Package archiveutil bounds ZIP directory metadata and decompressed entries.
// It reads containers in place and never extracts files to disk.
package archiveutil

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// ErrCapacity reports a container or entry exceeding the supported read budget.
var ErrCapacity = errors.New("archive exceeds read capacity")

const maxDirectoryBytes = 16 << 20
const maxEntries = 20000

// Reader owns the file behind a validated, bounded ZIP directory.
type Reader struct {
	*zip.Reader
	source *fileSource
}

type fileSource struct {
	mu   sync.RWMutex
	file *os.File
}

func (s *fileSource) ReadAt(data []byte, offset int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.file == nil {
		return 0, os.ErrClosed
	}
	return s.file.ReadAt(data, offset)
}

// Open accepts regular single-disk ZIP/ZIP64 containers, at most 20,000 entries
// and 16 MiB of central directory data, before allocating ZIP entry metadata.
func Open(path string) (_ *Reader, result error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("archive must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, f.Close())
		}
	}()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("archive changed while opening")
	}
	if err := validateDirectory(f, opened.Size()); err != nil {
		return nil, err
	}
	source := &fileSource{file: f}
	zr, err := zip.NewReader(source, opened.Size())
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(zr.File))
	for _, entry := range zr.File {
		if names[entry.Name] {
			return nil, errors.New("duplicate archive entry name")
		}
		names[entry.Name] = true
	}
	return &Reader{Reader: zr, source: source}, nil
}

// Close releases the underlying container file.
func (r *Reader) Close() error {
	r.source.mu.Lock()
	defer r.source.mu.Unlock()
	if r.source.file == nil {
		return nil
	}
	f := r.source.file
	r.source.file = nil
	return f.Close()
}

// Stat returns the identity of the open container, independent of path replacement.
func (r *Reader) Stat() (os.FileInfo, error) {
	r.source.mu.RLock()
	defer r.source.mu.RUnlock()
	if r.source.file == nil {
		return nil, os.ErrClosed
	}
	return r.source.file.Stat()
}

// Reopen attaches the same unchanged regular file to retained ZIP metadata,
// without reparsing its directory. Callers must hold an exclusive reader lease.
// Identity, size and mtime must match the metadata's original file version.
func (r *Reader) Reopen(path string, expected os.FileInfo) error {
	if expected == nil {
		return errors.New("archive identity is required")
	}
	r.source.mu.Lock()
	defer r.source.mu.Unlock()
	if r.source.file != nil {
		return errors.New("archive is already open")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(expected, info) || info.Size() != expected.Size() || !info.ModTime().Equal(expected.ModTime()) {
		return errors.New("archive version changed")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(expected, opened) || opened.Size() != expected.Size() || !opened.ModTime().Equal(expected.ModTime()) {
		return errors.Join(errors.New("archive changed while reopening"), err, f.Close())
	}
	r.source.file = f
	return nil
}

func validateDirectory(r io.ReaderAt, size int64) error {
	if size < 22 {
		return zip.ErrFormat
	}
	tail := make([]byte, min(size, 65535+22))
	if _, err := r.ReadAt(tail, size-int64(len(tail))); err != nil {
		return err
	}
	end := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) == 0x06054b50 && i+22+int(binary.LittleEndian.Uint16(tail[i+20:])) == len(tail) {
			end = i
			break
		}
	}
	if end < 0 {
		return zip.ErrFormat
	}
	e := tail[end:]
	if binary.LittleEndian.Uint16(e[4:]) != 0 || binary.LittleEndian.Uint16(e[6:]) != 0 {
		return errors.New("multi-disk archives are unsupported")
	}
	count := uint64(binary.LittleEndian.Uint16(e[10:]))
	length := uint64(binary.LittleEndian.Uint32(e[12:]))
	offset := uint64(binary.LittleEndian.Uint32(e[16:]))
	endOffset := size - int64(len(tail)) + int64(end)
	directoryEnd := endOffset
	if count == 0xffff || length == 0xffffffff || offset == 0xffffffff {
		var locator [20]byte
		if endOffset < 20 {
			return zip.ErrFormat
		}
		if _, err := r.ReadAt(locator[:], endOffset-20); err != nil {
			return err
		}
		if binary.LittleEndian.Uint32(locator[:]) != 0x07064b50 || binary.LittleEndian.Uint32(locator[4:]) != 0 || binary.LittleEndian.Uint32(locator[16:]) != 1 {
			return zip.ErrFormat
		}
		position := binary.LittleEndian.Uint64(locator[8:])
		if position > uint64(endOffset-20) || uint64(endOffset-20)-position < 56 {
			return zip.ErrFormat
		}
		var record [56]byte
		if _, err := r.ReadAt(record[:], int64(position)); err != nil {
			return err
		}
		if binary.LittleEndian.Uint32(record[:]) != 0x06064b50 || binary.LittleEndian.Uint32(record[16:]) != 0 || binary.LittleEndian.Uint32(record[20:]) != 0 {
			return zip.ErrFormat
		}
		recordLength := binary.LittleEndian.Uint64(record[4:])
		if recordLength < 44 || recordLength > 65536 || position+12+recordLength != uint64(endOffset-20) {
			return zip.ErrFormat
		}
		count = binary.LittleEndian.Uint64(record[32:])
		if binary.LittleEndian.Uint64(record[24:]) != count {
			return zip.ErrFormat
		}
		length = binary.LittleEndian.Uint64(record[40:])
		offset = binary.LittleEndian.Uint64(record[48:])
		directoryEnd = int64(position)
	} else if uint64(binary.LittleEndian.Uint16(e[8:])) != count {
		return zip.ErrFormat
	}
	if count > maxEntries || length > maxDirectoryBytes {
		return ErrCapacity
	}
	if offset > uint64(directoryEnd) || length != uint64(directoryEnd)-offset {
		return zip.ErrFormat
	}
	// Verify actual records, not only the untrusted count/size in the footer.
	dir := io.NewSectionReader(r, int64(offset), int64(length))
	for range count {
		var header [46]byte
		if _, err := io.ReadFull(dir, header[:]); err != nil {
			return zip.ErrFormat
		}
		if binary.LittleEndian.Uint32(header[:]) != 0x02014b50 {
			return zip.ErrFormat
		}
		extra := int64(binary.LittleEndian.Uint16(header[28:])) + int64(binary.LittleEndian.Uint16(header[30:])) + int64(binary.LittleEndian.Uint16(header[32:]))
		if _, err := dir.Seek(extra, io.SeekCurrent); err != nil {
			return err
		}
		position, err := dir.Seek(0, io.SeekCurrent)
		if err != nil || position > int64(length) {
			return zip.ErrFormat
		}
	}
	position, err := dir.Seek(0, io.SeekCurrent)
	if err != nil || position != int64(length) {
		return zip.ErrFormat
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// ReadEntry verifies the declared and actual decompressed size, cancellation and
// ZIP checksum, returning no partial content on failure. limit is a byte budget.
func ReadEntry(ctx context.Context, f *zip.File, limit int64) ([]byte, error) {
	if limit <= 0 || limit > 32<<20 || f.UncompressedSize64 > uint64(limit) {
		return nil, ErrCapacity
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, r: rc}, limit+1))
	err = errors.Join(readErr, rc.Close())
	if err != nil {
		return nil, fmt.Errorf("read archive entry: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, ErrCapacity
	}
	return data, nil
}
