package manga

import (
	"context"
	"errors"
	"os"
	"sync"
)

const readerCacheBudget = 8 << 20
const readerCacheItemBudget = 4 << 20
const maxOpenReaders = 8

type cachedReader struct {
	reader   *CBZReader
	identity os.FileInfo
	path     string
	refs     int
	bytes    int
	used     uint64
	keep     bool
}

// ReaderCache reuses immutable CBZ directory metadata for at most eight books.
// Descriptors exist only while leased, so idle caches do not hold media mounted.
// Retained metadata has an estimated 8MiB budget; startup does no I/O.
type ReaderCache struct {
	mu       sync.Mutex
	current  map[string]*cachedReader
	all      map[*cachedReader]bool
	sequence uint64
	closed   bool
	open     func(string) (*CBZReader, error)
}

// ReaderLease keeps a cached container open until Close, even if its path changes.
// Reader must not be used after Close; its directory data is immutable.
type ReaderLease struct {
	Reader *CBZReader
	cache  *ReaderCache
	entry  *cachedReader
	once   sync.Once
	err    error
}

// NewReaderCache creates an empty generation-scoped cache.
func NewReaderCache() *ReaderCache {
	return &ReaderCache{current: make(map[string]*cachedReader), all: make(map[*cachedReader]bool), open: OpenCBZ}
}

func readerBytes(r *CBZReader) int {
	bytes := len(r.allImages) * 16
	for _, f := range r.zr.File {
		bytes += 256 + len(f.Name) + len(f.Extra) + len(f.Comment)
	}
	return bytes
}

func (c *ReaderCache) remove(entry *cachedReader) error {
	if c.current[entry.path] == entry {
		delete(c.current, entry.path)
	}
	delete(c.all, entry)
	return entry.reader.Close()
}

func (c *ReaderCache) oldestIdle() *cachedReader {
	var oldest *cachedReader
	for entry := range c.all {
		if entry.refs == 0 && (oldest == nil || entry.used < oldest.used) {
			oldest = entry
		}
	}
	return oldest
}

// Acquire validates the path's file identity/size/mtime before reusing metadata.
// Replaced containers retire safely; oversized metadata is kept only for its lease.
// If all eight readers are leased, admission fails instead of opening unbounded FDs.
func (c *ReaderCache) Acquire(ctx context.Context, path string) (*ReaderLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identity, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !identity.Mode().IsRegular() {
		return nil, errors.New("CBZ must be a regular file")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("reader cache is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.sequence++
	if existing := c.current[path]; existing != nil {
		if os.SameFile(identity, existing.identity) && identity.Size() == existing.identity.Size() && identity.ModTime().Equal(existing.identity.ModTime()) {
			if existing.refs == 0 {
				if err := existing.reader.zr.Reopen(path, existing.identity); err != nil {
					return nil, err
				}
			}
			existing.refs++
			existing.used = c.sequence
			return &ReaderLease{Reader: existing.reader, cache: c, entry: existing}, nil
		}
		delete(c.current, path)
		existing.keep = false
		if existing.refs == 0 {
			if err := c.remove(existing); err != nil {
				return nil, err
			}
		}
	}
	for len(c.all) >= maxOpenReaders {
		oldest := c.oldestIdle()
		if oldest == nil {
			return nil, errors.New("CBZ reader capacity in use")
		}
		if err := c.remove(oldest); err != nil {
			return nil, err
		}
	}
	reader, err := c.open(path)
	if err != nil {
		return nil, err
	}
	opened, err := reader.zr.Stat()
	if err != nil || ctx.Err() != nil || !os.SameFile(identity, opened) || opened.Size() != identity.Size() || !opened.ModTime().Equal(identity.ModTime()) {
		return nil, errors.Join(errors.New("CBZ changed or request canceled while opening"), err, ctx.Err(), reader.Close())
	}
	entry := &cachedReader{reader: reader, identity: opened, path: path, refs: 1, bytes: readerBytes(reader), used: c.sequence}
	entry.keep = entry.bytes <= readerCacheItemBudget
	if entry.keep {
		retained := entry.bytes
		for other := range c.all {
			if other.keep {
				retained += other.bytes
			}
		}
		for retained > readerCacheBudget {
			oldest := c.oldestIdle()
			if oldest == nil {
				entry.keep = false
				break
			}
			if oldest.keep {
				retained -= oldest.bytes
			}
			if err := c.remove(oldest); err != nil {
				return nil, errors.Join(err, reader.Close())
			}
		}
	}
	c.all[entry] = true
	if entry.keep {
		c.current[path] = entry
	}
	return &ReaderLease{Reader: reader, cache: c, entry: entry}, nil
}

// Close releases the lease exactly once. A retired/uncached reader closes only
// after its final borrower exits, preventing LRU eviction from breaking requests.
func (lease *ReaderLease) Close() error {
	lease.once.Do(func() {
		c := lease.cache
		c.mu.Lock()
		defer c.mu.Unlock()
		lease.entry.refs--
		if lease.entry.refs == 0 {
			if !lease.entry.keep || c.closed {
				lease.err = c.remove(lease.entry)
			} else {
				lease.err = lease.entry.reader.Close()
			}
		}
	})
	return lease.err
}

// Close rejects new acquisitions and closes idle readers. Active leases finish
// safely and close on release; normal application shutdown drains them first.
func (c *ReaderCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	var errs []error
	for entry := range c.all {
		entry.keep = false
		if entry.refs == 0 {
			errs = append(errs, c.remove(entry))
		}
	}
	return errors.Join(errs...)
}
