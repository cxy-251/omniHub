// Package dlassets 把下载任务脚本（有声书 yt-dlp、漫画 jmcomic）和 uv 项目文件嵌入服务端，
// 运行时解压到缓存目录，由 uv 按 uv.lock 锁定的版本建虚拟环境（Python 只作按需子进程）。
package dlassets

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
)

//go:embed pyproject.toml uv.lock scripts/audiobook.py scripts/manga.py
var files embed.FS

var names = []string{"pyproject.toml", "uv.lock", "scripts/audiobook.py", "scripts/manga.py"}

// Prepare 把文件写到 dir（内容不变时不重写），返回 dir。
func Prepare(dir string) (string, error) {
	for _, name := range names {
		data, err := files.ReadFile(name)
		if err != nil {
			return "", err
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if old, err := os.ReadFile(target); err == nil && bytes.Equal(old, data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}
