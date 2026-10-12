package shortvideo

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// StreamVideo 处理短视频的 Range 206 局部内容请求（第 2.1.2 节）。
func StreamVideo(w http.ResponseWriter, r *http.Request, filePath string) error {
	cleanPath := filepath.Clean(filePath)
	file, err := os.Open(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return nil
		}
		return fmt.Errorf("failed to open video file: %w", err)
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat video file: %w", err)
	}

	if fi.IsDir() {
		return errors.New("target video path is a directory")
	}

	// 设置通用视频响应头
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "video/mp4")

	// http.ServeContent 自动处理 Range 206、If-Modified-Since 及内核级传输
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), file)
	return nil
}
