package audio

import (
	"context"
	"errors"
	"os"
	"sync"
)

const metadataCacheEntries = 32
const metadataCacheBytes = 8 << 20
const metadataCacheItemBytes = 4 << 20

// ErrMetadataCacheClosed rejects reads after the owning service generation ends.
var ErrMetadataCacheClosed = errors.New("audio metadata cache closed")

type metadataEntry struct {
	info  os.FileInfo
	value Metadata
	bytes int
	used  uint64
}

// MetadataCache retains at most 32 results/8MiB of labels and embedded pictures.
// Idle entries have no file handles. Cold parses are serialized and cancellable;
// repeated requests recheck identity/size/mtime without reopening or parsing tags.
// Entries above 4MiB are returned to their caller but not retained.
type MetadataCache struct {
	mu       sync.Mutex
	gate     chan struct{}
	entries  map[string]*metadataEntry
	bytes    int
	sequence uint64
	closed   bool
	parse    func(context.Context, *os.File) (Metadata, error)
}

// NewMetadataCache creates an empty cache without file I/O or background work.
func NewMetadataCache() *MetadataCache {
	return &MetadataCache{gate: make(chan struct{}, 1), entries: make(map[string]*metadataEntry), parse: ReadMetadata}
}

func sameMetadataFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func copyMetadata(value Metadata, picture bool) Metadata {
	if picture {
		value.Picture = append([]byte(nil), value.Picture...)
	} else {
		value.Picture = nil
	}
	return value
}

func (c *MetadataCache) lookup(path string, info os.FileInfo, picture bool) (Metadata, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Metadata{}, false, ErrMetadataCacheClosed
	}
	entry := c.entries[path]
	if entry == nil {
		return Metadata{}, false, nil
	}
	if !sameMetadataFile(entry.info, info) {
		delete(c.entries, path)
		c.bytes -= entry.bytes
		return Metadata{}, false, nil
	}
	c.sequence++
	entry.used = c.sequence
	return copyMetadata(entry.value, picture), true, nil
}

// Get verifies the current regular file before reuse. Metadata-only callers do
// not receive picture bytes; cover callers receive an independent byte slice.
// Replacements with preserved size/mtime still invalidate by file identity.
func (c *MetadataCache) Get(ctx context.Context, path string, picture bool) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if len(path) > 4096 {
		return Metadata{}, errors.New("audio path exceeds capacity")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Metadata{}, err
	}
	if !info.Mode().IsRegular() {
		return Metadata{}, errors.New("audio must be a regular file")
	}
	if value, ok, err := c.lookup(path, info, picture); ok || err != nil {
		return value, err
	}
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return Metadata{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	// Another request may have filled the entry while this caller waited.
	info, err = os.Lstat(path)
	if err != nil {
		return Metadata{}, err
	}
	if !info.Mode().IsRegular() {
		return Metadata{}, errors.New("audio must be a regular file")
	}
	if value, ok, err := c.lookup(path, info, picture); ok || err != nil {
		return value, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Metadata{}, err
	}
	opened, err := file.Stat()
	if err != nil || !sameMetadataFile(info, opened) {
		return Metadata{}, errors.Join(errors.New("audio changed while opening"), err, file.Close())
	}
	value, parseErr := c.parse(ctx, file)
	after, statErr := file.Stat()
	err = errors.Join(parseErr, statErr, file.Close(), ctx.Err())
	if err != nil {
		return Metadata{}, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return Metadata{}, err
	}
	if !sameMetadataFile(opened, after) || !sameMetadataFile(after, current) {
		return Metadata{}, errors.New("audio changed during metadata read")
	}
	size := 256 + len(path) + len(value.Title) + len(value.Artist) + len(value.Album) + len(value.Picture)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Metadata{}, ErrMetadataCacheClosed
	}
	retained := size <= metadataCacheItemBytes
	if retained {
		for len(c.entries) >= metadataCacheEntries || c.bytes+size > metadataCacheBytes {
			var victim string
			var oldest uint64 = ^uint64(0)
			for key, entry := range c.entries {
				if entry.used < oldest {
					victim, oldest = key, entry.used
				}
			}
			c.bytes -= c.entries[victim].bytes
			delete(c.entries, victim)
		}
		c.sequence++
		c.entries[path] = &metadataEntry{info: after, value: value, bytes: size, used: c.sequence}
		c.bytes += size
	}
	c.mu.Unlock()
	if !retained && picture {
		// This parse has no shared owner: avoid duplicating a large uncached
		// image just to hand its exclusively owned bytes to the response.
		return value, nil
	}
	return copyMetadata(value, picture), nil
}

// Close drops retained metadata and rejects new reads. A running cold parse
// closes its own file and cannot repopulate the closed cache.
func (c *MetadataCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.entries = nil
	c.bytes = 0
}
