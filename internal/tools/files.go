package tools

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SharedFile 描述共享目录中的文件属性。
type SharedFile struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
	IsDir     bool   `json:"is_dir"`
}

// ListSharedFiles 列出共享文件夹下的全部条目。
func ListSharedFiles(sharedDir string) ([]SharedFile, error) {
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create shared dir: %w", err)
	}

	entries, err := os.ReadDir(sharedDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read shared dir: %w", err)
	}

	var files []SharedFile
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, SharedFile{
			Name:      entry.Name(),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().Format("2006-01-02 15:04:05"),
			IsDir:     entry.IsDir(),
		})
	}
	return files, nil
}

// WriteChunk 将上传的分块追加写入目标文件（第 11.2.1 节分块上传）。
func WriteChunk(sharedDir, filename string, chunkIndex int, chunkData []byte) error {
	cleanName := filepath.Base(filename)
	if cleanName == "." || cleanName == "/" || strings.Contains(cleanName, "..") {
		return fmt.Errorf("invalid filename: %s", filename)
	}

	filePath := filepath.Join(sharedDir, cleanName)

	var flag int
	if chunkIndex == 0 {
		flag = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	} else {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}

	f, err := os.OpenFile(filePath, flag, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for chunk write: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(chunkData); err != nil {
		return fmt.Errorf("failed to append chunk data: %w", err)
	}
	return nil
}

// StreamZipArchive 将共享目录下的选定文件动态打包为 ZIP 流输出，零临时文件。
func StreamZipArchive(sharedDir string, filenames []string, w io.Writer) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, name := range filenames {
		cleanName := filepath.Base(name)
		filePath := filepath.Join(sharedDir, cleanName)

		fi, err := os.Stat(filePath)
		if err != nil || fi.IsDir() {
			continue
		}

		f, err := os.Open(filePath)
		if err != nil {
			continue
		}

		header, err := zip.FileInfoHeader(fi)
		if err != nil {
			_ = f.Close()
			continue
		}
		header.Name = cleanName
		header.Method = zip.Deflate

		writer, err := zw.CreateHeader(header)
		if err != nil {
			_ = f.Close()
			continue
		}

		_, _ = io.Copy(writer, f)
		_ = f.Close()
	}

	return nil
}
