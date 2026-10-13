package shortvideo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	catalogBudget         = 8 << 20
	channelBudget         = 2 << 20
	maxChannelDirectories = 2048
	maxChannelVideos      = 20000
)

// ErrNoVideos 表示频道内没有可播放的普通 MP4 文件。
var ErrNoVideos = errors.New("no videos in channel")

type directorySnapshot struct {
	modified time.Time
	children []string
	videos   []string
}

type channelSnapshot struct {
	directories map[string]directorySnapshot
	videos      []string
	checked     time.Time
	used        uint64
	bytes       int
}

// VideoCatalog 按需缓存频道目录快照；不写磁盘、不启动后台扫描。
// 每 10 秒最多检查一次目录时间戳，只重读改变的目录；缓存总预算 8MiB。
// 超出单频道限制返回明确错误，避免不完整索引或无限内存增长。
type VideoCatalog struct {
	mu       sync.Mutex
	channels map[string]*channelSnapshot
	clock    func() time.Time
	readDir  func(string) ([]os.DirEntry, error)
	sequence uint64
}

// NewVideoCatalog 创建空的按需目录缓存，启动时不访问资源库。
func NewVideoCatalog() *VideoCatalog {
	return &VideoCatalog{channels: make(map[string]*channelSnapshot), clock: time.Now, readDir: readBoundedDirectory}
}

func readBoundedDirectory(path string) (entries []os.DirEntry, result error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	entries, err = f.ReadDir(maxChannelVideos + maxChannelDirectories + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxChannelVideos+maxChannelDirectories {
		return nil, errors.New("video directory exceeds entry limit")
	}
	return entries, nil
}

// Random 返回相对于频道根目录的随机视频路径；忽略隐藏目录及符号链接。
// ctx 用于取消目录刷新，失败不会将部分扫描结果提交到缓存。
func (c *VideoCatalog) Random(ctx context.Context, root string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	now := c.clock()
	old := c.channels[root]
	if old == nil || now.Sub(old.checked) >= 10*time.Second {
		snapshot, err := c.refresh(ctx, root, old)
		if err != nil {
			return "", err
		}
		snapshot.checked = now
		delete(c.channels, root)
		for c.cachedBytes()+snapshot.bytes > catalogBudget || len(c.channels) >= 32 {
			var oldest string
			var used uint64 = ^uint64(0)
			for key, entry := range c.channels {
				if entry.used < used {
					oldest, used = key, entry.used
				}
			}
			delete(c.channels, oldest)
		}
		c.channels[root] = snapshot
		old = snapshot
	}
	c.sequence++
	old.used = c.sequence
	if len(old.videos) == 0 {
		return "", ErrNoVideos
	}
	return old.videos[rand.IntN(len(old.videos))], nil
}

// Invalidate 丢弃指定频道快照，供已删除文件或导入完成后刷新使用。
func (c *VideoCatalog) Invalidate(root string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.channels, root)
}

func (c *VideoCatalog) cachedBytes() int {
	total := 0
	for _, channel := range c.channels {
		total += channel.bytes
	}
	return total
}

func (c *VideoCatalog) refresh(ctx context.Context, root string, old *channelSnapshot) (*channelSnapshot, error) {
	next := &channelSnapshot{directories: make(map[string]directorySnapshot), bytes: len(root) + 256}
	pending := []string{"."}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rel := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		dir := filepath.Join(root, rel)
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) && rel != "." {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect video directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("video directory is not a physical directory")
		}
		var snapshot directorySnapshot
		var exists bool
		if old != nil {
			snapshot, exists = old.directories[rel]
		}
		if !exists || !snapshot.modified.Equal(info.ModTime()) {
			entries, err := c.readDir(dir)
			if err != nil {
				return nil, fmt.Errorf("read video directory: %w", err)
			}
			if len(entries) > maxChannelVideos+maxChannelDirectories {
				return nil, errors.New("video directory exceeds entry limit")
			}
			snapshot = directorySnapshot{modified: info.ModTime()}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				path := filepath.Join(rel, entry.Name())
				if entry.IsDir() {
					snapshot.children = append(snapshot.children, path)
				} else if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".mp4") {
					snapshot.videos = append(snapshot.videos, path)
				}
			}
		}
		next.directories[rel] = snapshot
		next.bytes += len(rel) + 128
		for _, child := range snapshot.children {
			next.bytes += len(child) + 64
		}
		for _, video := range snapshot.videos {
			next.bytes += len(video)*2 + 96
		}
		next.videos = append(next.videos, snapshot.videos...)
		pending = append(pending, snapshot.children...)
		if next.bytes > channelBudget || len(next.videos) > maxChannelVideos || len(next.directories)+len(pending) > maxChannelDirectories {
			return nil, errors.New("video channel exceeds bounded catalog capacity")
		}
	}
	return next, nil
}
