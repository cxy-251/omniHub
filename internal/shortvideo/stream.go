package shortvideo

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// StreamVideo 处理短视频的 Range 206 局部内容请求（第 2.1.2 节）。
func StreamVideo(w http.ResponseWriter, r *http.Request, filePath, contentType string) (result error) {
	cleanPath := filepath.Clean(filePath)
	info, err := os.Lstat(cleanPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("video must be a regular file")
	}
	file, err := os.Open(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return nil
		}
		return fmt.Errorf("failed to open video file: %w", err)
	}
	defer func() { result = errors.Join(result, file.Close()) }()

	fi, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat video file: %w", err)
	}

	if !fi.Mode().IsRegular() || !os.SameFile(info, fi) {
		return errors.New("video changed while opening")
	}

	// 设置通用视频响应头
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", contentType)

	// http.ServeContent 自动处理 Range 206、If-Modified-Since 及内核级传输
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), file)
	return nil
}
