// Package text 实现小说与技术文档的统一文本流化与大文件切片引擎。
// 遵循 ARCHITECTURE.md 第 6.1 节规范。
package text

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/archiveutil"
)

// SupportedExtensions 定义支持的文本文件后缀。
var SupportedExtensions = map[string]bool{
	".epub": true,
	".txt":  true,
	".md":   true,
	".rst":  true,
}

// ChunkResult 包含大文本分块切片结果。
type ChunkResult struct {
	Lines      []string `json:"lines"`
	StartLine  int      `json:"start_line"`
	TotalLines int      `json:"total_lines"`
	HasMore    bool     `json:"has_more"`
	NextOffset int64    `json:"next_offset"`
}

// ErrBounds reports invalid line, byte offset or page size parameters.
var ErrBounds = errors.New("invalid text chunk bounds")

const maxChunkBytes = 2 << 20
const maxLineBytes = 1 << 20

// ReadTextChunk 从纯文本、Markdown 或 RST 文件中按行范围分块读取（避免一次性加载超大文本导致内存暴涨）。
func ReadTextChunk(filePath string, startLine, limit int) (*ChunkResult, error) {
	return ReadTextChunkContext(context.Background(), filePath, startLine, limit, -1)
}

// ReadTextChunkContext reads at most 500 lines/2 MiB and stops at the page boundary.
// A nonnegative byteOffset resumes without rescanning the preceding file contents.
// TotalLines is -1 until EOF; callers use HasMore and NextOffset for navigation.
func ReadTextChunkContext(ctx context.Context, filePath string, startLine, limit int, byteOffset int64) (chunk *ChunkResult, result error) {
	if startLine == 0 {
		startLine = 1
	}
	if limit == 0 {
		limit = 100 // 默认每块 100 行
	}
	if startLine < 1 || startLine > 10000000 || limit < 1 || limit > 500 || byteOffset < -1 {
		return nil, ErrBounds
	}

	cleanPath := filepath.Clean(filePath)
	ext := strings.ToLower(filepath.Ext(cleanPath))
	if !SupportedExtensions[ext] || ext == ".epub" {
		return nil, fmt.Errorf("unsupported chunk text format: %s", ext)
	}

	info, err := os.Lstat(cleanPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("text must be a regular file")
	}
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open text file: %w", err)
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("text changed while opening")
	}
	if byteOffset > opened.Size() {
		return nil, ErrBounds
	}
	position := int64(0)
	currentLine := 0
	if byteOffset >= 0 {
		if _, err := file.Seek(byteOffset, io.SeekStart); err != nil {
			return nil, err
		}
		position = byteOffset
		currentLine = startLine - 1
	}
	reader := bufio.NewReaderSize(file, 32<<10)
	lines := make([]string, 0, limit)
	bytes := 0
	for len(lines) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := readLine(ctx, reader)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if len(line) == 0 && errors.Is(err, io.EOF) {
			break
		}
		// Roll back the last line if it would overflow this page's byte budget.
		// The client can request that line directly using NextOffset.
		if currentLine >= startLine-1 && bytes+len(line) > maxChunkBytes {
			return &ChunkResult{Lines: lines, StartLine: startLine, TotalLines: -1, HasMore: true, NextOffset: position}, nil
		}
		position += int64(len(line))
		currentLine++
		if currentLine >= startLine {
			bytes += len(line)
			lines = append(lines, strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r"))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, err = reader.Peek(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	more := err == nil
	total := -1
	if !more {
		total = currentLine
	}
	return &ChunkResult{
		Lines:      lines,
		StartLine:  startLine,
		TotalLines: total,
		HasMore:    more,
		NextOffset: position,
	}, nil
}

// Read long lines in small buffers, so a short page does not prefetch 1 MiB.
func readLine(ctx context.Context, reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > maxLineBytes {
			return nil, errors.New("text line exceeds 1 MiB")
		}
		line = append(line, part...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

// ReadEpubEntry 从 EPUB (ZIP) 容器中按内部相对路径直接提取单章节 XHTML 内容。
func ReadEpubEntry(epubPath, entryName string) ([]byte, error) {
	return ReadEpubEntryContext(context.Background(), epubPath, entryName)
}

// ReadEpubEntryContext reads at most 8 MiB of one EPUB entry without extracting it.
func ReadEpubEntryContext(ctx context.Context, epubPath, entryName string) (data []byte, result error) {
	cleanPath := filepath.Clean(epubPath)
	if strings.ToLower(filepath.Ext(cleanPath)) != ".epub" {
		return nil, errors.New("unsupported epub format")
	}
	zr, err := archiveutil.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open epub container: %w", err)
	}
	defer func() { result = errors.Join(result, zr.Close()) }()

	for _, file := range zr.File {
		if file.Name == entryName {
			return archiveutil.ReadEntry(ctx, file, 8<<20)
		}
	}

	return nil, errors.New("epub entry not found")
}
