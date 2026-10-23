// Package staging 管理资源库根目录下的暂存区 staging/：新下载的内容先放这里，筛选后再移入资源库。
//
// staging/ 不属于任何分类，扫描与索引都不碰它，所以下载、整理时不会让运行中的应用卡顿。
// 一次下载或一次收件是一个「批次」：staging/<批次名>/，里面放 batch.json（来源说明），
// 其余文件按「相对资源库根目录」的路径摆放（如 media_library/shortvideo/抖音/<作者>/x.mp4），
// 移入资源库就是把这棵树并进库根目录；同一块盘上是改名，瞬间完成，跨盘时复制后删除。
package staging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Dir 是暂存区在资源库根目录下的名字。
const Dir = "staging"

const manifest = "batch.json"

// Batch 是一个批次的概要。
type Batch struct {
	Name    string         `json:"name"`
	Label   string         `json:"label"`
	Source  string         `json:"source"`
	Created time.Time      `json:"created"`
	Files   int            `json:"files"`
	Bytes   int64          `json:"bytes"`
	Kinds   map[string]int `json:"kinds"` // video | image | audio | other
}

// File 是批次里的一个文件；Path 相对批次目录（斜杠分隔），Abs 是本机路径（桌面端直接预览）。
type File struct {
	Path     string    `json:"path"`
	Abs      string    `json:"abs"`
	Size     int64     `json:"size"`
	Kind     string    `json:"kind"`
	Modified time.Time `json:"modified"`
}

// Kind 按扩展名分类。
func Kind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mkv", ".webm", ".mov", ".m4v", ".avi", ".flv":
		return "video"
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif", ".heic":
		return "image"
	case ".mp3", ".m4a", ".m4b", ".opus", ".ogg", ".flac", ".wav", ".aac":
		return "audio"
	}
	return "other"
}

// NewBatch 建一个新批次目录，返回批次名。
func NewBatch(root, label, source string) (string, error) {
	name := time.Now().Format("2006-01-02 150405") + " " + sanitize(label)
	dir := filepath.Join(root, Dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(map[string]any{"label": label, "source": source, "created": time.Now()}, "", "  ")
	return name, os.WriteFile(filepath.Join(dir, manifest), data, 0o644)
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
	if len([]rune(s)) > 40 {
		s = string([]rune(s)[:40])
	}
	return strings.TrimSpace(s)
}

// BatchDir 校验批次名并返回批次目录。
func BatchDir(root, batch string) (string, error) {
	if batch == "" || batch == "." || batch == ".." || strings.ContainsAny(batch, `/\`) || strings.ContainsRune(batch, 0) {
		return "", errors.New("批次名无效")
	}
	dir := filepath.Join(root, Dir, batch)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return "", errors.New("批次不存在")
	}
	return dir, nil
}

// Resolve 把批次内的相对路径换成本机路径（不允许 .. 和绝对路径）。
func Resolve(root, batch, rel string) (string, error) {
	dir, err := BatchDir(root, batch)
	if err != nil {
		return "", err
	}
	clean := path.Clean("/" + rel)
	if clean == "/" || strings.ContainsRune(rel, 0) {
		return "", errors.New("路径无效")
	}
	return filepath.Join(dir, filepath.FromSlash(clean[1:])), nil
}

// List 列出资源库的全部批次（新的在前）。
func List(root string) ([]Batch, error) {
	entries, err := os.ReadDir(filepath.Join(root, Dir))
	if errors.Is(err, fs.ErrNotExist) {
		return []Batch{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Batch{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, Dir, e.Name())
		b := Batch{Name: e.Name(), Label: e.Name(), Kinds: map[string]int{}}
		if data, err := os.ReadFile(filepath.Join(dir, manifest)); err == nil {
			var m struct {
				Label, Source string
				Created       time.Time
			}
			if json.Unmarshal(data, &m) == nil {
				b.Label, b.Source, b.Created = m.Label, m.Source, m.Created
			}
		}
		if b.Created.IsZero() {
			if info, err := e.Info(); err == nil {
				b.Created = info.ModTime()
			}
		}
		_ = walkFiles(dir, func(rel string, info fs.FileInfo) {
			b.Files++
			b.Bytes += info.Size()
			b.Kinds[Kind(rel)]++
		})
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// walkFiles 遍历批次里的普通文件（跳过 batch.json 与符号链接），rel 为斜杠路径。
func walkFiles(dir string, fn func(rel string, info fs.FileInfo)) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == manifest {
			return nil
		}
		if info, err := d.Info(); err == nil {
			fn(filepath.ToSlash(rel), info)
		}
		return nil
	})
}

// Files 分页列出批次里的文件（按路径排序）；kind 非空时只列这一类。
func Files(root, batch, kind string, offset, limit int) ([]File, int, error) {
	dir, err := BatchDir(root, batch)
	if err != nil {
		return nil, 0, err
	}
	var all []File
	_ = walkFiles(dir, func(rel string, info fs.FileInfo) {
		k := Kind(rel)
		if kind == "" || k == kind {
			all = append(all, File{Path: rel, Abs: filepath.Join(dir, filepath.FromSlash(rel)), Size: info.Size(), Kind: k, Modified: info.ModTime()})
		}
	})
	sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	total := len(all)
	if offset > total {
		offset = total
	}
	end := min(total, offset+limit)
	return all[offset:end], total, nil
}

// ImportResult 是移入资源库的结果。
type ImportResult struct {
	Moved    int      `json:"moved"`
	Existing int      `json:"existing"` // 资源库里已有同名文件，留在批次里
	Refused  int      `json:"refused"`  // 不在 media_library/ 下，不移动
	Areas    []string `json:"areas"`    // 涉及的分类目录（如 media_library/shortvideo），用来触发扫描
	Errors   []string `json:"errors,omitempty"`
}

// Import 把批次并进资源库根目录。只移动 media_library/ 下的文件；同名文件已存在时不覆盖、留在批次里。
// 批次移空后删除批次目录。progress(已处理, 总数) 每个文件调用一次。
func Import(ctx context.Context, root, batch string, progress func(done, total int)) (ImportResult, error) {
	var res ImportResult
	dir, err := BatchDir(root, batch)
	if err != nil {
		return res, err
	}
	var files []string
	_ = walkFiles(dir, func(rel string, _ fs.FileInfo) { files = append(files, rel) })
	areas := map[string]bool{}
	for i, rel := range files {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		parts := strings.SplitN(rel, "/", 3)
		if len(parts) < 3 || parts[0] != "media_library" {
			res.Refused++
		} else {
			dst := filepath.Join(root, filepath.FromSlash(rel))
			if _, err := os.Lstat(dst); err == nil {
				res.Existing++
			} else if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				res.fail(rel, err)
			} else if err := moveFile(filepath.Join(dir, filepath.FromSlash(rel)), dst); err != nil {
				res.fail(rel, err)
			} else {
				res.Moved++
				areas[parts[0]+"/"+parts[1]] = true
			}
		}
		if progress != nil {
			progress(i+1, len(files))
		}
	}
	for a := range areas {
		res.Areas = append(res.Areas, a)
	}
	sort.Strings(res.Areas)
	removeEmptyDirs(dir)
	if left := 0; walkFiles(dir, func(string, fs.FileInfo) { left++ }) == nil && left == 0 {
		_ = os.Remove(filepath.Join(dir, manifest))
		_ = os.Remove(dir)
	}
	return res, nil
}

func (r *ImportResult) fail(rel string, err error) {
	if len(r.Errors) < 20 {
		r.Errors = append(r.Errors, rel+"："+err.Error())
	}
}

// moveFile 同盘改名；跨盘（EXDEV）时复制、落盘后删除源文件。目标已存在时不覆盖。
func moveFile(src, dst string) error {
	// 先建硬链接再删源：目标已存在时链接失败，不会覆盖（os.Rename 会覆盖）。
	switch err := os.Link(src, dst); {
	case err == nil:
		return os.Remove(src)
	case errors.Is(err, fs.ErrExist):
		return err
	case !errors.Is(err, syscall.EXDEV) && !isLinkUnsupported(err):
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".omnihub-part"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := errors.Join(out.Sync(), out.Close()); err != nil {
		os.Remove(tmp)
		return err
	}
	if info, err := in.Stat(); err == nil {
		_ = os.Chtimes(tmp, info.ModTime(), info.ModTime())
	}
	if _, err := os.Lstat(dst); err == nil {
		os.Remove(tmp)
		return fs.ErrExist
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Remove(src)
}

// isLinkUnsupported：exFAT/FAT 等不支持硬链接的文件系统（SD 卡常见）。
func isLinkUnsupported(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP)
}

// removeEmptyDirs 自底向上删除空目录（不删 dir 本身）。
func removeEmptyDirs(dir string) {
	var dirs []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != dir {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
}

// ErrNothing 表示没有可收的内容。
var ErrNothing = errors.New("没有可收的内容")

// Describe 生成一行中文结果说明。
func (r ImportResult) Describe() string {
	s := fmt.Sprintf("移入 %d 个文件", r.Moved)
	if r.Existing > 0 {
		s += fmt.Sprintf("，%d 个资源库里已有同名文件、留在暂存区", r.Existing)
	}
	if r.Refused > 0 {
		s += fmt.Sprintf("，%d 个不在 media_library 下、没有移动", r.Refused)
	}
	if len(r.Errors) > 0 {
		s += fmt.Sprintf("，%d 个出错", len(r.Errors))
	}
	return s
}
