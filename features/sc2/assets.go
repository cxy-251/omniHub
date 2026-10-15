// Package sc2assets 把 SC2 对战助手（Python 脚本、proton-wine 垫片、uv 项目文件）嵌入服务端，
// 运行时解压到配置目录，由 uv 按锁定版本建虚拟环境（规格 0.4：Python 只作按需子进程）。
package sc2assets

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
)

//go:embed sc2tool.py proton-wine pyproject.toml uv.lock
var files embed.FS

// Prepare 把文件写到 dir（内容不变时不重写），返回 dir。proton-wine 需可执行。
func Prepare(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	for _, name := range []string{"sc2tool.py", "proton-wine", "pyproject.toml", "uv.lock"} {
		data, err := files.ReadFile(name)
		if err != nil {
			return "", err
		}
		target := filepath.Join(dir, name)
		if old, err := os.ReadFile(target); err == nil && bytes.Equal(old, data) {
			continue
		}
		mode := os.FileMode(0644)
		if name == "proton-wine" {
			mode = 0755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return "", err
		}
		if err := os.Chmod(target, mode); err != nil {
			return "", err
		}
	}
	return dir, nil
}
