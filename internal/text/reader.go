// Package text 实现小说与技术文档的统一文本流化与大文件切片引擎。
// 遵循 ARCHITECTURE.md 第 6.1 节规范。
package text

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
}

// ReadTextChunk 从纯文本、Markdown 或 RST 文件中按行范围分块读取（避免一次性加载超大文本导致内存暴涨）。
func ReadTextChunk(filePath string, startLine, limit int) (*ChunkResult, error) {
	if startLine < 1 {
		startLine = 1
	}
	if limit <= 0 {
		limit = 100 // 默认每块 100 行
	}

	cleanPath := filepath.Clean(filePath)
	ext := strings.ToLower(filepath.Ext(cleanPath))
	if !SupportedExtensions[ext] || ext == ".epub" {
		return nil, fmt.Errorf("unsupported chunk text format: %s", ext)
	}

	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open text file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	currentLine := 0
	lines := make([]string, 0, limit)
	endLine := startLine + limit - 1

	for scanner.Scan() {
		currentLine++
		if currentLine >= startLine && currentLine <= endLine {
			lines = append(lines, scanner.Text())
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan text file: %w", err)
	}

	return &ChunkResult{
		Lines:      lines,
		StartLine:  startLine,
		TotalLines: currentLine,
		HasMore:    currentLine > endLine,
	}, nil
}

// ReadEpubEntry 从 EPUB (ZIP) 容器中按内部相对路径直接提取单章节 XHTML 内容。
func ReadEpubEntry(epubPath, entryName string) ([]byte, error) {
	cleanPath := filepath.Clean(epubPath)
	zr, err := zip.OpenReader(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open epub container: %w", err)
	}
	defer zr.Close()

	for _, file := range zr.File {
		if file.Name == entryName {
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}

	return nil, errors.New("epub entry not found")
}
