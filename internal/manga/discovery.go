package manga

import (
	"context"
	"errors"
	"os"
	"sync"
)

const discoveryBudget = 4 << 20
const discoveryEntries = 128

type discoveredBook struct {
	identity os.FileInfo
	book     *Manga
	err      error
	bytes    int
	used     uint64
}

// DiscoveryCache retains only bounded book metadata, never descriptors, images
// or disk indexes. File identity/size/mtime are checked on each active request.
type DiscoveryCache struct {
	mu       sync.Mutex
	entries  map[string]discoveredBook
	sequence uint64
	closed   bool
}

// NewDiscoveryCache performs no startup I/O or background polling.
func NewDiscoveryCache() *DiscoveryCache {
	return &DiscoveryCache{entries: map[string]discoveredBook{}}
}

func cloneBook(book *Manga) *Manga {
	if book == nil {
		return nil
	}
	copy := *book
	copy.Tags = append([]string(nil), book.Tags...)
	return &copy
}
func unchanged(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// Metadata reads one regular CBZ under caller-validated storage boundaries. The
// caller must bound concurrent reads; cancellation is never retained as an error.
func (c *DiscoveryCache) Metadata(ctx context.Context, path, relative string) (*Manga, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identity, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !identity.Mode().IsRegular() {
		return nil, errors.New("discovered book must be a regular file")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("manga discovery closed")
	}
	c.sequence++
	if entry, ok := c.entries[path]; ok && unchanged(identity, entry.identity) {
		entry.used = c.sequence
		c.entries[path] = entry
		c.mu.Unlock()
		book := cloneBook(entry.book)
		if book != nil {
			book.RelPath = relative
			book.ID = PathIdentity(relative)
		}
		return book, entry.err
	}
	delete(c.entries, path)
	c.mu.Unlock()
	book, readErr := ScanMangaFromCBZContext(ctx, path)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !unchanged(identity, current) {
		return nil, errors.New("book changed during metadata read")
	}
	bytes := 512 + len(path) + len(relative)
	if book != nil {
		book.RelPath = relative
		book.ID = PathIdentity(relative)
		book.UpdatedAt = identity.ModTime().UTC()
		bytes += 2 * (len(book.ID) + len(book.Title) + len(book.Author) + len(book.Description) + len(book.CustomCover))
		for _, tag := range book.Tags {
			bytes += 16 + 2*len(tag)
		}
	}
	if readErr != nil {
		bytes += len(readErr.Error())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("manga discovery closed")
	}
	var filesystemError *os.PathError
	if bytes <= discoveryBudget && !errors.As(readErr, &filesystemError) {
		c.sequence++
		c.entries[path] = discoveredBook{identity, cloneBook(book), readErr, bytes, c.sequence}
		for {
			total := 0
			oldest := ""
			var used uint64
			for key, entry := range c.entries {
				total += entry.bytes
				if oldest == "" || entry.used < used {
					oldest = key
					used = entry.used
				}
			}
			if len(c.entries) <= discoveryEntries && total <= discoveryBudget {
				break
			}
			delete(c.entries, oldest)
		}
	}
	return cloneBook(book), readErr
}

// Close releases generation metadata without touching source files.
func (c *DiscoveryCache) Close() { c.mu.Lock(); defer c.mu.Unlock(); c.closed = true; c.entries = nil }
