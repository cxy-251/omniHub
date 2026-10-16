package tools

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// SharedFile 描述共享目录中的文件属性。
type SharedFile struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
	IsDir     bool   `json:"is_dir"`
}

// ErrInvalidName 表示文件名不是共享目录里的一个普通名字（含路径分隔符、以点开头等）。
var ErrInvalidName = errors.New("invalid shared file name")

const partPrefix = ".upload-"

// validName 只接受共享目录下一层的普通名字；以点开头的留给隐藏文件和未完成的上传。
func validName(name string) bool {
	return name != "" && len(name) <= 255 && !strings.HasPrefix(name, ".") && !strings.ContainsAny(name, "/\\\x00") && path.Base(name) == name
}

// ListSharedFiles 列出共享目录下的条目（隐藏文件与未完成的上传不列出）。
func ListSharedFiles(sharedDir string) ([]SharedFile, error) {
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create shared dir: %w", err)
	}
	entries, err := os.ReadDir(sharedDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read shared dir: %w", err)
	}
	files := []SharedFile{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, SharedFile{Name: entry.Name(), SizeBytes: info.Size(), ModTime: info.ModTime().Format("2006-01-02 15:04:05"), IsDir: entry.IsDir()})
	}
	return files, nil
}

// WriteChunk 把一块上传数据追加到 .upload-<名字> 临时文件；last 为真时改成正式名字。
// 所有文件操作经 os.Root，不会被符号链接带出共享目录；正式名字已存在时返回 fs.ErrExist。
func WriteChunk(sharedDir, filename string, chunkIndex int, last bool, data io.Reader) (resultErr error) {
	if !validName(filename) {
		return ErrInvalidName
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(sharedDir)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	if _, err := root.Lstat(filename); err == nil {
		return fs.ErrExist
	}
	part := partPrefix + filename
	flag := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if chunkIndex == 0 {
		flag = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := root.OpenFile(part, flag, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, data); err != nil {
		_ = f.Close()
		_ = root.Remove(part)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if !last {
		return nil
	}
	if _, err := root.Lstat(filename); err == nil {
		_ = root.Remove(part)
		return fs.ErrExist
	}
	return root.Rename(part, filename)
}

// OpenShared 打开共享目录里的一个普通文件用于下载。
func OpenShared(sharedDir, name string) (*os.File, fs.FileInfo, error) {
	if !validName(name) {
		return nil, nil, ErrInvalidName
	}
	root, err := os.OpenRoot(sharedDir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, fs.ErrNotExist
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	return f, info, nil
}

// StreamZipArchive 把共享目录下的指定文件打包为 ZIP 流输出，不落临时文件。
func StreamZipArchive(sharedDir string, filenames []string, w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, name := range filenames {
		f, info, err := OpenShared(sharedDir, name)
		if err != nil {
			continue
		}
		header, err := zip.FileInfoHeader(info)
		if err == nil {
			header.Name, header.Method = name, zip.Deflate
			var out io.Writer
			if out, err = zw.CreateHeader(header); err == nil {
				_, err = io.Copy(out, f)
			}
		}
		_ = f.Close()
		if err != nil {
			return errors.Join(err, zw.Close())
		}
	}
	return zw.Close()
}
