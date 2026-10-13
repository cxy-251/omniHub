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
		if err := c.CheckStateAvailability(); err != nil {
			return "", false, err
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
	if primary != nil && path == filepath.Join(primary.RootPath, library.DataDirName) {
		c.StateLibraryRoot, c.StateLibraryID = primary.RootPath, primary.ID
	}
	return path, true, nil
}

func hasStateDatabase(dir string) (bool, error) {
	info, err := os.Lstat(filepath.Join(dir, "users.db"))
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

// CheckStateAvailability 只检查固定状态位置和介质身份，不修改配置或创建文件。
func (c *Config) CheckStateAvailability() error {
	if c.StateDirectory == "" {
		return errors.New("global state location not resolved")
	}
	if c.StateLibraryRoot != "" {
		if c.IsLibraryReadOnly(c.StateLibraryRoot) {
			return errors.New("global state medium is configured read-only; select a writable state location")
		}
		marker, err := library.ReadMarker(c.StateLibraryRoot)
		if err != nil || marker.ID != c.StateLibraryID {
			return errors.New("global state medium identity unavailable or changed")
		}
	}
	if c.StateInitialized {
		return requireStateDatabase(c.StateDirectory)
	}
	return nil
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
