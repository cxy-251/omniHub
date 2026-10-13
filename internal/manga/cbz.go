package manga

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	zr        *archiveutil.Reader
	filePath  string
	entryMap  map[string]*zip.File
	allImages []string
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
		reader.entryMap[f.Name] = f
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ImageExtensions[ext] {
			reader.allImages = append(reader.allImages, f.Name)
		}
	}

	sort.Slice(reader.allImages, func(i, j int) bool { return ordering.NaturalLess(reader.allImages[i], reader.allImages[j]) })
	return reader, nil
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
	if entryName == "metadata.json" {
		limit = 1 << 20
	}
	return archiveutil.ReadEntry(ctx, f, limit)
}

// ParseMetadata 提取解析 metadata.json 元数据。
func (r *CBZReader) ParseMetadata() (*PackageMetadata, error) {
	data, err := r.ReadEntry("metadata.json")
	if err != nil {
		return nil, err
	}

	var meta PackageMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
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
		base := strings.ToLower(filepath.Base(img))
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

// ReadCoverBytes 提取封面图片数据流。
func (r *CBZReader) ReadCoverBytes(customCover string) ([]byte, error) {
	entry := r.GetCoverEntry(customCover)
	if entry == "" {
		return nil, errors.New("no cover image available in cbz")
	}
	return r.ReadEntry(entry)
}

// ScanMangaFromCBZ 扫描单个 CBZ 构建 Manga 元数据实体。
func ScanMangaFromCBZ(cbzPath string) (_ *Manga, result error) {
	r, err := OpenCBZ(cbzPath)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, r.Close()) }()

	stem := strings.TrimSuffix(filepath.Base(cbzPath), filepath.Ext(cbzPath))
	manga := &Manga{
		ID:         stem,
		Title:      stem,
		RelPath:    filepath.Base(cbzPath),
		TotalPages: len(r.allImages),
		UpdatedAt:  time.Now().UTC(),
	}

	meta, err := r.ParseMetadata()
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
