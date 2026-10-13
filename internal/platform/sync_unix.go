//go:build !windows

package platform

import (
	"errors"
	"os"
)

// SyncDirectory 同步目录项，保证迁移日志和配置替换在断电后仍可恢复。
func SyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// ReplaceFile 替换应用拥有的配置文件；调用方应先同步临时文件内容。
func ReplaceFile(source, destination string) error { return os.Rename(source, destination) }
