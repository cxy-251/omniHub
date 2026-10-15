package shortvideo

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"omnihub/internal/ordering"
)

// 单个目录的条目上限，防止异常目录把扫描拖进无界内存。
const maxDirEntries = 50000

var imageExtensions = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

// 下载插件的命名：2024-12-05_标题_7444881751299657018
var namePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})_(.*?)(?:_([0-9a-f]{16,}|\d{10,}))?$`)

// ScanStats 汇总一次扫描的变化。
type ScanStats struct {
	Videos  int `json:"videos"`
	Albums  int `json:"albums"`
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
}

type scannedPost = Post

// Scan 增量同步 dir 下的目录树到 index.db：只写入新增、变化与消失的作品。
// 已入库且原目录已清理的图集保留记录（图片从 albums.db 读取）。
func (s *Store) Scan(ctx context.Context, dir string) (ScanStats, error) {
	var stats ScanStats
	if s.readOnly {
		return stats, errors.New("read-only library cannot be indexed")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return stats, err
	}
	defer root.Close()

	known, err := s.postSignatures()
	if err != nil {
		return stats, err
	}
	seen := make(map[string]bool, len(known))
	creators := make(map[string]Creator)

	platforms, err := readDir(root, ".")
	if err != nil {
		return stats, err
	}
	for _, platform := range platforms {
		if !platform.IsDir() || hidden(platform.Name()) {
			continue
		}
		creatorDirs, err := readDir(root, platform.Name())
		if err != nil {
			return stats, err
		}
		for _, creatorDir := range creatorDirs {
			if err := ctx.Err(); err != nil {
				return stats, err
			}
			if !creatorDir.IsDir() || hidden(creatorDir.Name()) {
				continue
			}
			creatorID := platform.Name() + "/" + creatorDir.Name()
			posts, err := scanCreator(root, creatorID)
			if err != nil {
				return stats, err
			}
			if len(posts) > 0 {
				creators[creatorID] = Creator{ID: creatorID, Platform: platform.Name(), Name: creatorDir.Name()}
			}
			var changed []scannedPost
			for _, p := range posts {
				seen[p.ID] = true
				if p.Kind == KindVideo {
					stats.Videos++
				} else {
					stats.Albums++
				}
				old, ok := known[p.ID]
				switch {
				case !ok:
					stats.Added++
				case old.Size != p.Size || old.MTime != p.MTime || old.ImageCount != p.ImageCount || old.Music != p.Music || !old.OnDisk:
					stats.Updated++
				default:
					continue
				}
				changed = append(changed, p)
			}
			if err := s.writePosts(changed, known); err != nil {
				return stats, err
			}
		}
	}

	// 消失的作品：视频与未入库图集删除记录；已入库图集标记为不在磁盘。
	var gone []string
	for id, old := range known {
		if seen[id] {
			continue
		}
		if old.Kind == KindAlbum && old.Imported {
			if old.OnDisk {
				if _, err := s.index.Exec("UPDATE posts SET on_disk=0 WHERE id=?", id); err != nil {
					return stats, err
				}
			}
			creators[old.CreatorID] = Creator{ID: old.CreatorID, Platform: platformOf(old.CreatorID), Name: path.Base(old.CreatorID)}
			continue
		}
		gone = append(gone, id)
	}
	if err := s.removePosts(gone); err != nil {
		return stats, err
	}
	stats.Removed = len(gone)
	return stats, s.replaceCreators(creators)
}

// scanCreator 列出一个博主目录下的视频与图集。
func scanCreator(root *os.Root, creatorID string) ([]scannedPost, error) {
	entries, err := readDir(root, creatorID)
	if err != nil {
		return nil, err
	}
	music := make(map[string]bool)
	for _, e := range entries {
		if e.Type().IsRegular() && strings.EqualFold(path.Ext(e.Name()), ".mp3") {
			music[strings.TrimSuffix(e.Name(), path.Ext(e.Name()))] = true
		}
	}
	var posts []scannedPost
	for _, e := range entries {
		name := e.Name()
		if hidden(name) {
			continue
		}
		id := creatorID + "/" + name
		switch {
		case e.Type().IsRegular() && strings.EqualFold(path.Ext(name), ".mp4"):
			info, err := e.Info()
			if err != nil {
				continue // 扫描期间被移走
			}
			p := newPost(id, creatorID, KindVideo, strings.TrimSuffix(name, path.Ext(name)))
			p.Size, p.MTime = info.Size(), info.ModTime().UnixNano()
			posts = append(posts, p)
		case e.IsDir():
			images, size, mtime, err := albumImages(root, id)
			if err != nil {
				return nil, err
			}
			if len(images) == 0 {
				continue
			}
			p := newPost(id, creatorID, KindAlbum, name)
			p.Size, p.MTime, p.ImageCount = size, mtime, len(images)
			if music[name] {
				p.Music = id + ".mp3"
			}
			posts = append(posts, p)
		}
	}
	return posts, nil
}

// albumImages 返回图集目录内按自然序排列的图片文件名及其总大小、最新修改时间。
func albumImages(root *os.Root, dir string) (names []string, size, mtime int64, err error) {
	entries, err := readDir(root, dir)
	if err != nil {
		return nil, 0, 0, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !imageExtensions[strings.ToLower(path.Ext(e.Name()))] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		names = append(names, e.Name())
		size += info.Size()
		mtime = max(mtime, info.ModTime().UnixNano())
	}
	sort.Slice(names, func(i, j int) bool { return ordering.NaturalLess(names[i], names[j]) })
	return names, size, mtime, nil
}

func newPost(id, creatorID string, kind Kind, base string) Post {
	p := Post{ID: id, CreatorID: creatorID, Kind: kind, Title: base, OnDisk: true}
	if m := namePattern.FindStringSubmatch(base); m != nil {
		p.Published = m[1]
		if strings.TrimSpace(m[2]) != "" {
			p.Title = m[2]
		}
	}
	return p
}

func readDir(root *os.Root, name string) ([]fs.DirEntry, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(maxDirEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxDirEntries {
		return nil, errors.New("directory exceeds entry limit: " + name)
	}
	return entries, nil
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") }

func platformOf(creatorID string) string {
	platform, _, _ := strings.Cut(creatorID, "/")
	return platform
}
