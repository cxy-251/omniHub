package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	catalogBudget  = 8 << 20
	rootBudget     = 2 << 20
	maxDirectories = 2048
	maxFiles       = 20000
)

// ErrNoFiles 表示目录内没有符合配置扩展名的普通文件。
var ErrNoFiles = errors.New("no files in root")

type directorySnapshot struct {
	modified time.Time
	identity os.FileInfo
	children []string
	files    []string
}

type rootSnapshot struct {
	directories map[string]directorySnapshot
	files       []string
	checked     time.Time
	used        uint64
	bytes       int
	folders     []Directory
}

// Directory 表示含有匹配文件的相对目录及其直接文件数量。
type Directory struct {
	Path  string
	Count int
}

// Catalog 按需缓存目录快照；不写磁盘、不启动后台扫描。
// 每 10 秒最多检查一次目录时间戳，只重读改变的目录；缓存总预算 8MiB。
// 超出单目录限制返回明确错误，避免不完整索引或无限内存增长。
type Catalog struct {
	mu         sync.Mutex
	roots      map[string]*rootSnapshot
	clock      func() time.Time
	readDir    func(*os.Root, string) ([]os.DirEntry, error)
	sequence   uint64
	extensions map[string]bool
}

// New 创建按扩展名筛选的空目录缓存，启动时不访问资源库；空筛选清单不收录文件。
func New(extensions ...string) *Catalog {
	allowed := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		allowed[strings.ToLower(ext)] = true
	}
	return &Catalog{roots: make(map[string]*rootSnapshot), clock: time.Now, readDir: readBoundedDirectory, extensions: allowed}
}

func readBoundedDirectory(root *os.Root, path string) (entries []os.DirEntry, result error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	entries, err = f.ReadDir(maxFiles + maxDirectories + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxFiles+maxDirectories {
		return nil, errors.New("file directory exceeds entry limit")
	}
	return entries, nil
}

// Random 返回相对于根目录的随机文件路径，使用正斜线；忽略隐藏项及符号链接。
// ctx 用于取消目录刷新，失败不会将部分扫描结果提交到缓存。
func (c *Catalog) Random(ctx context.Context, root string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot, err := c.snapshotLocked(ctx, root)
	if err != nil {
		return "", err
	}
	if len(snapshot.files) == 0 {
		return "", ErrNoFiles
	}
	return snapshot.files[rand.IntN(len(snapshot.files))], nil
}

// Page 返回稳定排序的相对路径副本与总数量，单次至多 200 条，不修改磁盘。
func (c *Catalog) Page(ctx context.Context, root string, limit, offset int) ([]string, int, error) {
	if limit < 1 || limit > 200 || offset < 0 {
		return nil, 0, errors.New("invalid catalog page bounds")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot, err := c.snapshotLocked(ctx, root)
	if err != nil {
		return nil, 0, err
	}
	total := len(snapshot.files)
	if offset >= total {
		return []string{}, total, nil
	}
	end := offset + min(limit, total-offset)
	return append([]string{}, snapshot.files[offset:end]...), total, nil
}

// DirectoryPage 分页返回含匹配文件的目录，用于嵌套音频专辑等视图。
func (c *Catalog) DirectoryPage(ctx context.Context, root string, limit, offset int) ([]Directory, int, error) {
	if limit < 1 || limit > 200 || offset < 0 {
		return nil, 0, errors.New("invalid directory page bounds")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot, err := c.snapshotLocked(ctx, root)
	if err != nil {
		return nil, 0, err
	}
	total := len(snapshot.folders)
	if offset >= total {
		return []Directory{}, total, nil
	}
	return append([]Directory{}, snapshot.folders[offset:offset+min(limit, total-offset)]...), total, nil
}

func (c *Catalog) snapshotLocked(ctx context.Context, root string) (*rootSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := c.clock()
	old := c.roots[root]
	if old == nil || now.Sub(old.checked) >= 10*time.Second {
		snapshot, err := c.refresh(ctx, root, old)
		if err != nil {
			return nil, err
		}
		snapshot.checked = now
		delete(c.roots, root)
		for c.cachedBytes()+snapshot.bytes > catalogBudget || len(c.roots) >= 512 {
			var oldest string
			var used uint64 = ^uint64(0)
			for key, entry := range c.roots {
				if entry.used < used {
					oldest, used = key, entry.used
				}
			}
			delete(c.roots, oldest)
		}
		c.roots[root] = snapshot
		old = snapshot
	}
	c.sequence++
	old.used = c.sequence
	return old, nil
}

// Invalidate 丢弃指定根目录快照，供已删除文件或导入完成后刷新使用。
func (c *Catalog) Invalidate(root string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.roots, root)
}

func (c *Catalog) cachedBytes() int {
	total := 0
	for _, root := range c.roots {
		total += root.bytes
	}
	return total
}

func (c *Catalog) refresh(ctx context.Context, root string, old *rootSnapshot) (next *rootSnapshot, result error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("catalog root must be a physical directory")
	}
	scope, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := scope.Close(); err != nil {
			next = nil
			result = errors.Join(result, err)
		}
	}()
	opened, err := scope.Stat(".")
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("catalog root changed while opening")
	}
	next = &rootSnapshot{directories: make(map[string]directorySnapshot), bytes: len(root) + 256}
	pending := []string{"."}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rel := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		info, err := scope.Lstat(rel)
		if errors.Is(err, os.ErrNotExist) && rel != "." {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect file directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("file directory is not a physical directory")
		}
		var snapshot directorySnapshot
		var exists bool
		if old != nil {
			snapshot, exists = old.directories[rel]
		}
		if !exists || !snapshot.modified.Equal(info.ModTime()) || !os.SameFile(snapshot.identity, info) {
			entries, err := c.readDir(scope, rel)
			if err != nil {
				return nil, fmt.Errorf("read file directory: %w", err)
			}
			if len(entries) > maxFiles+maxDirectories {
				return nil, errors.New("file directory exceeds entry limit")
			}
			snapshot = directorySnapshot{modified: info.ModTime(), identity: info}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				path := filepath.Join(rel, entry.Name())
				if entry.IsDir() {
					snapshot.children = append(snapshot.children, path)
				} else if entry.Type().IsRegular() && c.extensions[strings.ToLower(filepath.Ext(entry.Name()))] {
					snapshot.files = append(snapshot.files, filepath.ToSlash(path))
				}
			}
		}
		next.directories[rel] = snapshot
		next.bytes += len(rel) + 384
		for _, child := range snapshot.children {
			next.bytes += len(child) + 64
		}
		for _, file := range snapshot.files {
			next.bytes += len(file)*2 + 96
		}
		next.files = append(next.files, snapshot.files...)
		pending = append(pending, snapshot.children...)
		if next.bytes > rootBudget || len(next.files) > maxFiles || len(next.directories)+len(pending) > maxDirectories {
			return nil, errors.New("file root exceeds bounded catalog capacity")
		}
	}
	sort.Strings(next.files)
	counts := make(map[string]int)
	for _, file := range next.files {
		counts[filepath.ToSlash(filepath.Dir(file))]++
	}
	for dir, count := range counts {
		next.folders = append(next.folders, Directory{Path: dir, Count: count})
		next.bytes += len(dir) + 64
	}
	sort.Slice(next.folders, func(i, j int) bool { return next.folders[i].Path < next.folders[j].Path })
	if next.bytes > rootBudget {
		return nil, errors.New("directory groups exceed bounded catalog capacity")
	}
	return next, nil
}
