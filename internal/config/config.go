// Package config 提供 omniHub 的配置定义、默认值生成与持久化读写。
// 遵循 ARCHITECTURE.md 第 0.6 节规范，配置文件存放于系统标准路径（Linux: ~/.config/omnihub/config.json）。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	// PrimaryLibrary 是当前绑定的主媒体库 UUID 或路径标识。
	PrimaryLibrary string `json:"primary_library"`
	// Libraries 记录所有挂载的物理资源库根路径列表。
	Libraries []string `json:"libraries"`
	// ReadOnlyLibraries 指定只读资源库路径，与 Libraries/PrimaryLibrary 中的路径匹配。
	ReadOnlyLibraries []string `json:"read_only_libraries,omitempty"`
}

// IsLibraryReadOnly 查询指定资源库是否以只读模式配置。
func (c *Config) IsLibraryReadOnly(path string) bool {
	for _, candidate := range c.ReadOnlyLibraries {
		if filepath.Clean(candidate) == filepath.Clean(path) {
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
		PrimaryLibrary: "",
		Libraries:      []string{},
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
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d (must be between 1 and 65535)", c.Server.Port)
	}
	if c.Server.Host == "" {
		return errors.New("server host cannot be empty")
	}
	return nil
}

// Load 读取并解析系统配置文件。若文件不存在则返回默认配置。
func Load() (*Config, error) {
	path, err := GetConfigFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
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

	// 写入临时文件后重命名，防止写入中断导致损坏
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary config: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to commit config file: %w", err)
	}

	return nil
}
