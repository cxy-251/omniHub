package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"omnihub/internal/library"
)

// ResolveStateDirectory 确定全局状态的唯一位置，不因主库上线或离线切换账号库。
// 首次升级保留已存在的初始化数据库；多个旧位置都有账号时要求显式选择。
// 返回 changed 表示调用方必须持久化位置，再允许创建数据库。
func (c *Config) ResolveStateDirectory(configDir string, primary *library.Library) (path string, changed bool, err error) {
	if c.StateDirectory != "" {
		if !filepath.IsAbs(c.StateDirectory) {
			return "", false, errors.New("global state location must be absolute")
		}
		path = filepath.Clean(c.StateDirectory)
		if c.StateInitialized {
			if err := requireStateDatabase(path); err != nil {
				return "", false, err
			}
		}
		return path, false, nil
	}
	bootstrap, err := filepath.Abs(configDir)
	if err != nil {
		return "", false, fmt.Errorf("resolve initialization state: %w", err)
	}
	bootstrapExists, err := hasStateDatabase(bootstrap)
	if err != nil {
		return "", false, err
	}
	if c.PrimaryLibrary != "" && (primary == nil || !primary.IsOnline) {
		return "", false, errors.New("configured primary library is unavailable; cannot select legacy global state safely")
	}
	path = bootstrap
	if primary != nil && primary.IsOnline && !primary.ReadOnly {
		candidate := filepath.Join(primary.RootPath, library.DataDirName)
		candidateExists, err := hasStateDatabase(candidate)
		if err != nil {
			return "", false, err
		}
		if bootstrapExists && candidateExists && candidate != bootstrap {
			return "", false, errors.New("both initialization and primary library contain user databases; explicit state selection is required")
		}
		if !bootstrapExists {
			path = candidate
		}
	}
	c.StateDirectory = path
	return path, true, nil
}

func hasStateDatabase(dir string) (bool, error) {
	info, err := os.Stat(filepath.Join(dir, "users.db"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect user database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("user database must be a regular file")
	}
	return true, nil
}

func requireStateDatabase(dir string) error {
	exists, err := hasStateDatabase(dir)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("global state is unavailable; refusing to create replacement user database")
	}
	return nil
}
