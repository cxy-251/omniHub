package manga

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"omnihub/internal/archiveutil"
	"omnihub/internal/ordering"
)

// ImageExtensions defines the image entries exposed by the CBZ page endpoint.
var ImageExtensions = map[string]bool{
	".webp": true,
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
}

// ErrEntryNotFound distinguishes an absent optional metadata file from corruption.
var ErrEntryNotFound = errors.New("file not found in cbz archive")

// PackageMetadata 映射 metadata.json 结构。
type PackageMetadata struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Author         string   `json:"author"`
	Description    string   `json:"description"`
	Tags           []string `json:"tags"`
	OnlineChapters int      `json:"online_chapters"`
	LocalChapters  int      `json:"local_chapters"`
	IsComplete     bool     `json:"is_complete"`
}

// CBZReader 封装基于 ReaderAt 的 CBZ 零落盘随机寻址阅读器（第 7.1.3 节）。
type CBZReader struct {
	zr         *archiveutil.Reader
	filePath   string
	entryMap   map[string]*zip.File
	allImages  []string
	pages      []PageEntry
	coverMu    sync.Mutex
	coverKey   string
	coverEntry string
}

// PageEntry identifies one reading page; an independent root cover is excluded.
// Entry is an opaque URL-safe key when ZIP names contain non-UTF8/control bytes.
type PageEntry struct {
	Index        int    `json:"index"`
	ChapterIndex int    `json:"chapter_index"`
	Entry        string `json:"entry"`
}

const encodedEntryPrefix = "@zip:"

// EncodeEntry preserves ordinary names and encodes names JSON cannot round-trip.
func EncodeEntry(name string) string {
	encode := !utf8.ValidString(name) || strings.HasPrefix(name, encodedEntryPrefix) || strings.ContainsAny(name, "\"\\")
	for _, ch := range name {
		if ch < 32 {
			encode = true
			break
		}
	}
	if encode {
		return encodedEntryPrefix + base64.RawURLEncoding.EncodeToString([]byte(name))
	}
	return name
}

// DecodeEntry validates an opaque page key and recovers the exact ZIP name bytes.
func DecodeEntry(value string) (string, error) {
	if len(value) > 5500 {
		return "", errors.New("image entry exceeds capacity")
	}
	if strings.HasPrefix(value, encodedEntryPrefix) {
		data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, encodedEntryPrefix))
		if err != nil || len(data) > 4096 {
			return "", errors.New("invalid image entry key")
		}
		return string(data), nil
	}
	if len(value) > 4096 {
		return "", errors.New("image entry exceeds capacity")
	}
	return value, nil
}

// OpenCBZ 打开指定路径的 CBZ 封箱包。
func OpenCBZ(cbzPath string) (*CBZReader, error) {
	cleanPath := filepath.Clean(cbzPath)
	zr, err := archiveutil.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cbz file: %w", err)
	}

	reader := &CBZReader{
		zr:       zr,
		filePath: cleanPath,
		entryMap: make(map[string]*zip.File),
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if len(f.Name) > 4096 {
			return nil, errors.Join(errors.New("archive entry name exceeds capacity"), zr.Close())
		}
		reader.entryMap[f.Name] = f
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ImageExtensions[ext] {
			reader.allImages = append(reader.allImages, f.Name)
		}
	}

	sort.Slice(reader.allImages, func(i, j int) bool { return ordering.NaturalLess(reader.allImages[i], reader.allImages[j]) })
	chapters := make(map[string]int)
	for _, name := range reader.allImages {
		if !strings.Contains(name, "/") && strings.HasPrefix(strings.ToLower(name), "cover.") {
			continue
		}
		dir := path.Dir(name)
		chapter, found := chapters[dir]
		if !found {
			chapter = len(chapters)
			chapters[dir] = chapter
		}
		reader.pages = append(reader.pages, PageEntry{Index: len(reader.pages), ChapterIndex: chapter, Entry: EncodeEntry(name)})
	}
	return reader, nil
}

// PageWindow copies up to 100 reading page keys without allocating a whole-book response.
func (r *CBZReader) PageWindow(offset, limit int) ([]PageEntry, int, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, errors.New("invalid page window")
	}
	if offset >= len(r.pages) {
		return []PageEntry{}, len(r.pages), nil
	}
	return append([]PageEntry(nil), r.pages[offset:min(offset+limit, len(r.pages))]...), len(r.pages), nil
}

// Version identifies the currently leased container by size and modification time.
// This is a metadata revision, not a content hash.
func (r *CBZReader) Version() (string, error) {
	info, err := r.zr.Stat()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x-%x", info.Size(), info.ModTime().UnixNano()), nil
}

// ListImageEntries 返回包内所有经过自然排序的图片条目名称列表。
func (r *CBZReader) ListImageEntries() []string {
	return append([]string(nil), r.allImages...)
}

// Close 关闭 CBZ 文件句柄。
func (r *CBZReader) Close() error {
	return r.zr.Close()
}

// ReadEntry 随机寻址提取包内任意文件的二进制流，零落盘（第 7.1.3 节）。
func (r *CBZReader) ReadEntry(entryName string) ([]byte, error) {
	return r.ReadEntryContext(context.Background(), entryName)
}

// ReadEntryContext reads a single page within 32 MiB, respecting request cancellation.
// Metadata is restricted to 1 MiB; unsupported entry sizes return no partial data.
func (r *CBZReader) ReadEntryContext(ctx context.Context, entryName string) ([]byte, error) {
	f, ok := r.entryMap[entryName]
	if !ok {
		return nil, ErrEntryNotFound
	}

	limit := int64(32 << 20)
	if entryName == "metadata.json" || entryName == "ComicInfo.xml" {
		limit = 1 << 20
	}
	return archiveutil.ReadEntry(ctx, f, limit)
}

// GetCoverEntry 按照多级回退规范获取封面文件条目名（第 7.2 节）。
func (r *CBZReader) GetCoverEntry(customCover string) string {
	// 1. 手动指定封面优先
	if customCover != "" {
		if _, ok := r.entryMap[customCover]; ok && ImageExtensions[strings.ToLower(filepath.Ext(customCover))] {
			return customCover
		}
	}

	// 2. 根目录 cover.* 优先
	for _, img := range r.allImages {
		base := strings.ToLower(path.Base(img))
		if !strings.Contains(img, "/") && strings.HasPrefix(base, "cover.") {
			return img
		}
	}

	// 3. 回退至按自然序排布的第一张图片
	if len(r.allImages) > 0 {
		return r.allImages[0]
	}

	return ""
}

// ReadCoverBytes returns a verified cover using the bounded fallback policy.
func (r *CBZReader) ReadCoverBytes(customCover string) ([]byte, error) {
	return r.ReadCoverContext(context.Background(), customCover)
}

// ScanMangaFromCBZ 扫描单个 CBZ 构建 Manga 元数据实体。
func ScanMangaFromCBZ(cbzPath string) (*Manga, error) {
	return ScanMangaFromCBZContext(context.Background(), cbzPath)
}

// ScanMangaFromCBZContext reads one archive with cancellable metadata decoding.
func ScanMangaFromCBZContext(ctx context.Context, cbzPath string) (_ *Manga, result error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := OpenCBZ(cbzPath)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, r.Close()) }()

	stem := strings.TrimSuffix(filepath.Base(cbzPath), filepath.Ext(cbzPath))
	manga := &Manga{
		ID:           stem,
		Title:        stem,
		RelPath:      filepath.Base(cbzPath),
		TotalPages:   len(r.pages),
		ChapterCount: r.chapterCount(),
		UpdatedAt:    time.Now().UTC(),
	}

	meta, err := r.ParseMetadataContext(ctx)
	if err != nil && !errors.Is(err, ErrEntryNotFound) {
		return nil, fmt.Errorf("invalid manga metadata: %w", err)
	}
	if meta != nil {
		if meta.ID != "" {
			manga.ID = meta.ID
		}
		if meta.Title != "" {
			manga.Title = meta.Title
		}
		manga.Author = meta.Author
		manga.Description = meta.Description
		manga.Tags = meta.Tags
		manga.IsComplete = meta.IsComplete
		manga.ChapterCount = meta.LocalChapters
	}

	return manga, nil
}
