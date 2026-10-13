package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var categoryPattern = regexp.MustCompile(`^(media_library|standalone_games)/[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidateCategories 验证分类根目录清单，允许空列表停用所有分类，禁止重复或越界路径。
func ValidateCategories(dirs []string) error {
	if dirs == nil || len(dirs) > 128 {
		return errors.New("category directories require an explicit list of at most 128 entries")
	}
	seen := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		if !categoryPattern.MatchString(dir) || !filepath.IsLocal(filepath.FromSlash(dir)) || seen[dir] {
			return fmt.Errorf("invalid or duplicate category directory: %q", dir)
		}
		seen[dir] = true
	}
	return nil
}

// NewConfiguredManager 依据配置创建管理器；未启用的标准分类默认不可解析。
func NewConfiguredManager(dirs []string) (*Manager, error) {
	if err := ValidateCategories(dirs); err != nil {
		return nil, err
	}
	m := NewManager()
	m.skeletonDirs = append([]string{}, dirs...)
	active := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		active[dir] = true
	}
	for _, dir := range StandardDirs {
		if !active[dir] {
			m.disabledCategories[dir] = true
		}
	}
	return m, nil
}

// ActiveDirectories 返回配置中当前未停用的分类快照，不读取磁盘。
func (m *Manager) ActiveDirectories() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, 0, len(m.skeletonDirs))
	for _, dir := range m.skeletonDirs {
		if !m.disabledCategories[dir] {
			result = append(result, dir)
		}
	}
	return result
}

func ensureDirectories(rootPath string, dirs []string) (result error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	for _, dir := range dirs {
		// Root 的目录操作在打开期间约束符号链接，避免先检查再 MkdirAll 的竞态。
		if err := root.MkdirAll(filepath.FromSlash(dir), 0755); err != nil {
			return fmt.Errorf("create library directory %q: %w", dir, err)
		}
	}
	return nil
}
