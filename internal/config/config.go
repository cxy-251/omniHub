// Package config 提供 omniHub 的配置定义、默认值生成与持久化读写。
// 遵循 ARCHITECTURE.md 第 0.6 节规范，配置文件存放于系统标准路径（Linux: ~/.config/omnihub/config.json）。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"omnihub/internal/library"
	"omnihub/internal/platform"
)

// ServerConfig 定义 HTTP 服务端的监听与网络参数。
type ServerConfig struct {
	// Port 是服务监听的端口号，默认 8998。
	Port int `json:"port"`
	// Host 是服务监听的主机地址，默认 "0.0.0.0" 供局域网访问。
	Host string `json:"host"`
}

// Config 是 omniHub 的根配置结构体。
type Config struct {
	// Server 包含服务网络监听配置。
	Server ServerConfig `json:"server"`
	// PrimaryLibrary 是当前绑定的主媒体库绝对根路径；UUID 单独持久化。
	PrimaryLibrary string `json:"primary_library"`
	// Libraries 记录所有挂载的物理资源库根路径列表。
	Libraries []string `json:"libraries"`
	// ReadOnlyLibraries 指定只读资源库路径，与 Libraries/PrimaryLibrary 中的路径匹配。
	ReadOnlyLibraries []string `json:"read_only_libraries,omitempty"`
	// LibraryIdentities 按根路径保存稳定物理标识，离线启动不能重新生成 ID。
	LibraryIdentities map[string]library.Marker `json:"library_identities,omitempty"`
	// StateDirectory 固定全局状态所在目录，仅由显式主库迁移流程改变。
	StateDirectory string `json:"state_directory,omitempty"`
	// StateInitialized 标记用户数据库已落盘，介质丢失时禁止创建空白账号库。
	StateInitialized bool `json:"state_initialized,omitempty"`
	// StateLibraryRoot/ID 固定承载账号状态的介质身份，即使移除其媒体挂载也保留。
	StateLibraryRoot string `json:"state_library_root,omitempty"`
	StateLibraryID   string `json:"state_library_id,omitempty"`
	// CategoryDirectories 定义启用的分类目录；空列表停用所有分类，不删除内容。
	CategoryDirectories []string `json:"category_directories"`
	// DisabledCategories 保留已移除的自定义分类，供路径授权与重新启用使用。
	DisabledCategories []string `json:"disabled_categories,omitempty"`
}

var saveMu sync.Mutex

// CommittedWriteError 表示文件已替换，但目录同步失败；调用方不能当作未提交回滚数据。
type CommittedWriteError struct{ Cause error }

// Error 描述已替换配置但持久性同步失败的边界。
func (e *CommittedWriteError) Error() string {
	return "configuration replaced but directory sync failed: " + e.Cause.Error()
}

// Unwrap 返回底层同步错误，供 errors.Is/errors.As 检查。
func (e *CommittedWriteError) Unwrap() error { return e.Cause }

// WriteWasCommitted 判断错误是否发生在配置文件已经替换之后。
func WriteWasCommitted(err error) bool {
	var committed *CommittedWriteError
	return errors.As(err, &committed)
}

// KnownLibrary 返回路径对应的已记录标识副本，未记录时返回 nil。
func (c *Config) KnownLibrary(root string) *library.Marker {
	path, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	for root, marker := range c.LibraryIdentities {
		if library.SameRoot(root, path) {
			copy := marker
			return &copy
		}
	}
	return nil
}

// RememberLibrary 保存在线物理库身份，返回是否发生了配置变更。
func (c *Config) RememberLibrary(lib *library.Library) bool {
	if lib == nil || !lib.IsOnline {
		return false
	}
	marker := library.Marker{ID: lib.ID, Name: lib.Name}
	if c.LibraryIdentities[lib.RootPath] == marker {
		return false
	}
	if c.LibraryIdentities == nil {
		c.LibraryIdentities = make(map[string]library.Marker)
	}
	c.LibraryIdentities[lib.RootPath] = marker
	return true
}

// IsLibraryReadOnly 查询指定资源库是否以只读模式配置。
func (c *Config) IsLibraryReadOnly(path string) bool {
	for _, candidate := range c.ReadOnlyLibraries {
		if library.SameRoot(candidate, path) {
			return true
		}
	}
	return false
}

// DefaultConfig 返回包含安全默认值的配置实例。
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 8998,
			Host: "0.0.0.0",
		},
		PrimaryLibrary:      "",
		Libraries:           []string{},
		CategoryDirectories: append([]string{}, library.StandardDirs...),
	}
}

// GetConfigDir 获取符合当前操作系统规范的配置目录路径。
// Linux: ~/.config/omnihub
// Windows: %APPDATA%\omnihub
// macOS: ~/Library/Application Support/omnihub
func GetConfigDir() (string, error) {
	// 隔离开发实例与便携部署，路径仍由配置中心统一提供。
	if dir := os.Getenv("OMNIHUB_CONFIG_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", errors.New("OMNIHUB_CONFIG_DIR must be absolute")
		}
		return filepath.Clean(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "omnihub"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "omnihub"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "omnihub"), nil
	default: // linux, freebsd, etc. (XDG 规范)
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig != "" {
			return filepath.Join(xdgConfig, "omnihub"), nil
		}
		return filepath.Join(home, ".config", "omnihub"), nil
	}
}

// GetConfigFilePath 获取全局配置文件 config.json 的绝对路径。
func GetConfigFilePath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Validate 执行防御性配置有效性校验。
func (c *Config) Validate() error {
	if err := library.ValidateCategories(c.CategoryDirectories); err != nil {
		return err
	}
	if c.DisabledCategories != nil {
		if err := library.ValidateCategories(c.DisabledCategories); err != nil {
			return err
		}
	}
	active := make(map[string]bool, len(c.CategoryDirectories))
	for _, dir := range c.CategoryDirectories {
		active[dir] = true
	}
	for _, dir := range c.DisabledCategories {
		if active[dir] {
			return errors.New("category cannot be active and disabled simultaneously")
		}
	}
	if len(c.Libraries) > 128 || len(c.ReadOnlyLibraries) > 128 || len(c.LibraryIdentities) > 256 {
		return errors.New("too many configured libraries")
	}
	paths := append(append(append([]string{}, c.Libraries...), c.ReadOnlyLibraries...), c.PrimaryLibrary)
	for _, path := range paths {
		if path != "" && (!filepath.IsAbs(path) || len(path) > 4096) {
			return errors.New("library paths must be absolute and bounded")
		}
	}
	for root, marker := range c.LibraryIdentities {
		if !filepath.IsAbs(root) || len(root) > 4096 || marker.ID == "" || len(marker.ID) > 128 || strings.ContainsAny(marker.ID, "/\\:\x00") || len(marker.Name) > 256 {
			return errors.New("invalid configured library identity")
		}
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d (must be between 1 and 65535)", c.Server.Port)
	}
	if c.Server.Host == "" {
		return errors.New("server host cannot be empty")
	}
	if c.StateDirectory != "" && !filepath.IsAbs(c.StateDirectory) {
		return errors.New("state directory must be absolute")
	}
	if c.StateInitialized && c.StateDirectory == "" {
		return errors.New("initialized state requires a fixed directory")
	}
	if (c.StateLibraryRoot == "") != (c.StateLibraryID == "") {
		return errors.New("state medium requires both root and identity")
	}
	if c.StateLibraryRoot != "" && (!filepath.IsAbs(c.StateLibraryRoot) || filepath.Join(c.StateLibraryRoot, library.DataDirName) != c.StateDirectory) {
		return errors.New("state medium does not match global state location")
	}
	return nil
}

// Load 读取并解析系统配置文件。若文件不存在则返回默认配置。
func Load() (*Config, error) {
	path, err := GetConfigFilePath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在时，使用并保存默认配置
			cfg := DefaultConfig()
			if err := Save(cfg); err != nil {
				return nil, fmt.Errorf("save default configuration: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file from %s: %w", path, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err := errors.Join(readErr, f.Close()); err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	if len(data) > 2<<20 {
		return nil, errors.New("configuration exceeds size limit")
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config json: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Save 将配置实例安全写入磁盘。
func Save(cfg *Config) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	if cfg == nil {
		return errors.New("cannot save nil config")
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("cannot save invalid config: %w", err)
	}

	path, err := GetConfigFilePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return WritePrivateFile(path, data)
}

// WritePrivateFile 将应用拥有的配置或凭证通过独立临时文件同步后替换，权限为 0600。
// 临时文件属于内部写入事务，不是用户资源；失败时清理临时文件并保留原文件。
func WritePrivateFile(path string, data []byte) (result error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".omnihub-*.tmp")
	if err != nil {
		return fmt.Errorf("create configuration transaction: %w", err)
	}
	name := f.Name()
	defer func() {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("clean configuration transaction: %w", err))
		}
	}()
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return fmt.Errorf("persist configuration transaction: %w", err)
	}
	if err := platform.ReplaceFile(name, path); err != nil {
		return fmt.Errorf("commit configuration transaction: %w", err)
	}
	if err := platform.SyncDirectory(filepath.Dir(path)); err != nil {
		return &CommittedWriteError{Cause: err}
	}
	return nil
}
