package scanner

import (
	"context"
	"database/sql"
	"errors"
	"omnihub/internal/games"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const discoveryBudget = 8 << 20

type discoveryVersion struct {
	name string
	info os.FileInfo
}
type discoveryEntry struct {
	rows     []*games.Game
	versions []discoveryVersion
	checked  time.Time
	bytes    int
	used     uint64
}

// GameDiscovery caches unindexed catalogs within 8MiB. Work occurs only on
// requests: at most one scan; every ten seconds unchanged file identities and
// metadata timestamps reuse records. It never retains open files or writes.
type GameDiscovery struct {
	gate     chan struct{}
	mu       sync.Mutex
	entries  map[string]*discoveryEntry
	bytes    int
	sequence uint64
	closed   bool
}

// NewGameDiscovery creates an idle, empty cache.
func NewGameDiscovery() *GameDiscovery {
	return &GameDiscovery{gate: make(chan struct{}, 1), entries: make(map[string]*discoveryEntry)}
}

// Close discards metadata and prevents ongoing requests from repopulating it.
func (c *GameDiscovery) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	clear(c.entries)
	c.bytes = 0
}

// GameCatalog is an immutable borrowed view. Queries return independent records.
type GameCatalog struct{ entry *discoveryEntry }

// Open reads or reuses a rooted catalog, cancellably waiting for scan admission.
func (c *GameDiscovery) Open(ctx context.Context, directory string, categories []string) (*GameCatalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if categories == nil || len(categories) > 128 {
		return nil, errors.New("explicit discovery categories required")
	}
	categories = append([]string{}, categories...)
	for _, category := range categories {
		if !games.ValidCategory(category) {
			return nil, errors.New("invalid discovery category")
		}
	}
	sort.Strings(categories)
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := directory + "\x00" + strings.Join(categories, "\x00")
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("game discovery closed")
	}
	entry := c.entries[key]
	c.sequence++
	used := c.sequence
	c.mu.Unlock()
	now := time.Now()
	if entry != nil && now.Sub(entry.checked) < 10*time.Second {
		return c.touch(key, entry, used)
	}
	if entry != nil {
		unchanged, err := sameGameVersions(ctx, directory, entry.versions)
		if err != nil {
			return nil, err
		}
		if unchanged {
			c.mu.Lock()
			entry.checked = now
			c.mu.Unlock()
			return c.touch(key, entry, used)
		}
	}
	versions, err := gameVersions(ctx, directory, categories)
	if err != nil {
		return nil, err
	}
	rows, err := ReadGameMetadata(ctx, directory, categories)
	if err != nil {
		return nil, err
	}
	unchanged, err := sameGameVersions(ctx, directory, versions)
	if err != nil {
		return nil, err
	}
	if !unchanged {
		return nil, errors.New("game metadata changed during discovery; retry")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	size := 0
	for _, row := range rows {
		size += 256 + len(row.ID) + len(row.Name) + len(row.Category) + len(row.RelDir) + len(row.Exe) + len(row.Args) + len(row.Env) + len(row.Icon) + len(row.ProtonAppID)
	}
	for _, version := range versions {
		size += 160 + len(version.name)
	}
	fresh := &discoveryEntry{rows: rows, versions: versions, checked: now, bytes: size, used: used}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("game discovery closed")
	}
	if old := c.entries[key]; old != nil {
		c.bytes -= old.bytes
		delete(c.entries, key)
	}
	if size <= discoveryBudget {
		for c.bytes+size > discoveryBudget || len(c.entries) >= 32 {
			oldest := ""
			var order uint64 = ^uint64(0)
			for candidate, value := range c.entries {
				if value.used < order {
					oldest = candidate
					order = value.used
				}
			}
			c.bytes -= c.entries[oldest].bytes
			delete(c.entries, oldest)
		}
		c.entries[key] = fresh
		c.bytes += size
	}
	return &GameCatalog{entry: fresh}, nil
}
func (c *GameDiscovery) touch(key string, entry *discoveryEntry, used uint64) (*GameCatalog, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("game discovery closed")
	}
	entry.used = used
	return &GameCatalog{entry: entry}, nil
}

func sameGameVersions(ctx context.Context, directory string, versions []discoveryVersion) (result bool, resultErr error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	for _, version := range versions {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		info, err := root.Lstat(filepath.FromSlash(version.name))
		if errors.Is(err, os.ErrNotExist) {
			if version.info == nil {
				continue
			}
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if version.info == nil || !os.SameFile(version.info, info) || version.info.Size() != info.Size() || !version.info.ModTime().Equal(info.ModTime()) || version.info.Mode() != info.Mode() {
			return false, nil
		}
	}
	return true, nil
}
func gameVersions(ctx context.Context, directory string, categories []string) (versions []discoveryVersion, resultErr error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	versionBytes := 0
	add := func(name string) error {
		versionBytes += 160 + len(name)
		if versionBytes > discoveryBudget {
			return errors.New("discovery versions exceed memory budget")
		}
		info, err := root.Lstat(filepath.FromSlash(name))
		if errors.Is(err, os.ErrNotExist) {
			info = nil
			err = nil
		}
		if err != nil {
			return err
		}
		versions = append(versions, discoveryVersion{name: name, info: info})
		if len(versions) > 40129 {
			return errors.New("discovery versions exceed capacity")
		}
		return nil
	}
	if err := add("."); err != nil {
		return nil, err
	}
	for _, category := range categories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parent := category + "_games"
		if err := add(parent); err != nil {
			return nil, err
		}
		info := versions[len(versions)-1].info
		if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		entries, err := gameEntries(root, parent)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			name := parent + "/" + entry.Name()
			if err := add(name); err != nil {
				return nil, err
			}
			if err := add(name + "/omni.json"); err != nil {
				return nil, err
			}
		}
	}
	return versions, nil
}

// Close releases no resources: a view holds only immutable in-memory data.
func (c *GameCatalog) Close() error { return nil }

// GetGame returns a copy of one discovered record, without filesystem access.
func (c *GameCatalog) GetGame(ctx context.Context, id string) (*games.Game, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id == "" || len(id) > 512 {
		return nil, errors.New("invalid game identity")
	}
	index := sort.Search(len(c.entry.rows), func(i int) bool { return c.entry.rows[i].ID >= id })
	if index == len(c.entry.rows) || c.entry.rows[index].ID != id {
		return nil, sql.ErrNoRows
	}
	value := *c.entry.rows[index]
	return &value, nil
}

// ListCatalogPage returns at most 100 display records, omitting launch fields.
func (c *GameCatalog) ListCatalogPage(ctx context.Context, target, category, after string, limit int) ([]*games.Game, error) {
	if limit < 1 || limit > 100 || (target != "gui_only" && target != "web_only") || len(after) > 512 || category != "" && !games.ValidCategory(category) {
		return nil, errors.New("invalid catalog bounds")
	}
	result := make([]*games.Game, 0, limit)
	start := sort.Search(len(c.entry.rows), func(i int) bool { return c.entry.rows[i].ID > after })
	for _, row := range c.entry.rows[start:] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if row.Target != target || category != "" && row.Category != category {
			continue
		}
		value := *row
		value.Exe = ""
		value.Args = ""
		value.Env = ""
		value.ProtonAppID = ""
		result = append(result, &value)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}
