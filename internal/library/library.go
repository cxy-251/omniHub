// Package library 实现媒体库架构与生命周期管理。
// 遵循 ARCHITECTURE.md 第 1 节规范，支持 0 库容错、多库平级聚合、
// 结构动态补全、删除即软停用、UUID 物理标识及库内相对路径映射。
package library

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LibraryMarkerFile 是放置在资源库根目录下的物理库标识文件名。
const LibraryMarkerFile = "omnilibrary.json"

// DataDirName 是主媒体库根目录下存放系统数据库与状态的隐藏目录名。
const DataDirName = ".omni_data"

// StandardDirs 定义统一的资源库骨架子目录清单。
var StandardDirs = []string{
	// 独立游戏与模拟器分类
	"standalone_games/rpg_games",
	"standalone_games/retro_games",
	"standalone_games/slg_games",
	"standalone_games/flash_games",
	"standalone_games/steam_games",
	"standalone_games/unity_games",
	"standalone_games/godot_games",
	"standalone_games/unreal_games",
	"standalone_games/wine_games",
	"standalone_games/3ds_games",
	"standalone_games/app_games",
	// 媒体专区分类
	"media_library/docs",
	"media_library/manga",
	"media_library/novels",
	"media_library/audio",
	"media_library/shortvideo",
	"media_library/gallery",
	"media_library/shared",
}

// Marker 记录资源库的元数据标识。
type Marker struct {
	// ID 是资源库的全局唯一 UUID。
	ID string `json:"id"`
	// Name 是人类可读的库名称。
	Name string `json:"name"`
}

// Library 表示已加载的单个物理资源库实例。
type Library struct {
	// ID 是从标识文件中读取的 UUID。
	ID string `json:"id"`
	// Name 是库友好名称。
	Name string `json:"name"`
	// RootPath 是当前机器上的绝对根路径。
	RootPath string `json:"root_path"`
	// IsPrimary 标记是否为主媒体库。
	IsPrimary bool `json:"is_primary"`
	// IsOnline 标记该物理介质当前是否可访问。
	IsOnline bool `json:"is_online"`
}

// Manager 统管系统中挂载的所有资源库，线程安全。
type Manager struct {
	mu                 sync.RWMutex
	libraries          map[string]*Library // key: library ID
	primaryID          string
	disabledCategories map[string]bool // 软停用分类相对目录（第 1.2.3 节）
}

// NewManager 创建空的媒体库管理器实例，天然满足 0 库安全容错。
func NewManager() *Manager {
	return &Manager{
		libraries:          make(map[string]*Library),
		disabledCategories: make(map[string]bool),
	}
}

// generateUUID 生成 16 字节十六进制随机字符串作为资源库唯一标识。
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// EnsureMarker 检查并确保指定根目录下存在物理标识文件，不存在则自动生成。
func EnsureMarker(rootPath string, defaultName string) (*Marker, error) {
	markerPath := filepath.Join(rootPath, LibraryMarkerFile)
	data, err := os.ReadFile(markerPath)
	if err == nil {
		var m Marker
		if err := json.Unmarshal(data, &m); err == nil && m.ID != "" {
			return &m, nil
		}
	}

	if defaultName == "" {
		defaultName = filepath.Base(rootPath)
	}

	m := &Marker{
		ID:   generateUUID(),
		Name: defaultName,
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal library marker: %w", err)
	}

	if err := os.WriteFile(markerPath, raw, 0644); err != nil {
		return nil, fmt.Errorf("failed to write library marker: %w", err)
	}

	return m, nil
}

// EnsureSkeleton 依据统一规范为资源库补全骨架目录（遵循第 1.2.2 节）。
func EnsureSkeleton(rootPath string) error {
	for _, dir := range StandardDirs {
		target := filepath.Join(rootPath, filepath.FromSlash(dir))
		if err := os.MkdirAll(target, 0755); err != nil {
			return fmt.Errorf("failed to create directory skeleton %s: %w", target, err)
		}
	}
	return nil
}

// AddLibrary 挂载并校验一个物理资源库根目录。
func (m *Manager) AddLibrary(rootPath string, isPrimary bool) (*Library, error) {
	if rootPath == "" {
		return nil, errors.New("library root path cannot be empty")
	}

	cleanPath := filepath.Clean(rootPath)

	// 检查目录是否存在与在线状态（第 1.4.3 节）
	info, err := os.Stat(cleanPath)
	isOnline := err == nil && info.IsDir()

	var marker *Marker
	if isOnline {
		// 在线状态下确保标识文件与骨架存在
		marker, err = EnsureMarker(cleanPath, "")
		if err != nil {
			return nil, fmt.Errorf("failed to verify marker in %s: %w", cleanPath, err)
		}
		if err := EnsureSkeleton(cleanPath); err != nil {
			return nil, fmt.Errorf("failed to verify skeleton in %s: %w", cleanPath, err)
		}
	} else {
		// 离线状态下分配临时标记
		marker = &Marker{
			ID:   "offline-" + cleanPath,
			Name: filepath.Base(cleanPath),
		}
	}

	lib := &Library{
		ID:        marker.ID,
		Name:      marker.Name,
		RootPath:  cleanPath,
		IsPrimary: isPrimary,
		IsOnline:  isOnline,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.libraries[lib.ID] = lib
	if isPrimary || m.primaryID == "" {
		m.primaryID = lib.ID
		// 若为主库，确保 .omni_data 目录存在（第 1.3.1 节）
		if isOnline {
			dataDir := filepath.Join(cleanPath, DataDirName)
			_ = os.MkdirAll(dataDir, 0755)
		}
	}

	return lib, nil
}

// RemoveLibrary 移除指定资源库映射（不删除任何物理文件）。
func (m *Manager) RemoveLibrary(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.libraries, id)
	if m.primaryID == id {
		m.primaryID = ""
		// 自动挑选第一个可用库作为新主库
		for remainingID := range m.libraries {
			m.primaryID = remainingID
			m.libraries[remainingID].IsPrimary = true
			break
		}
	}
}

// GetPrimary 获取当前主媒体库实例。若未配置任何库返回 nil（满足 0 库容错）。
func (m *Manager) GetPrimary() *Library {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.primaryID == "" {
		return nil
	}
	return m.libraries[m.primaryID]
}

// ListLibraries 返回当前所有已挂载的资源库列表。
func (m *Manager) ListLibraries() []*Library {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*Library, 0, len(m.libraries))
	for _, lib := range m.libraries {
		list = append(list, lib)
	}
	return list
}

// SetCategoryDisabled 设置某分类目录的停用状态（第 1.2.3 节删除即软停用）。
func (m *Manager) SetCategoryDisabled(categoryRelPath string, disabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	normalized := filepath.ToSlash(filepath.Clean(categoryRelPath))
	if disabled {
		m.disabledCategories[normalized] = true
	} else {
		delete(m.disabledCategories, normalized)
	}
}

// IsCategoryDisabled 判断某分类目录当前是否处于停用状态。
func (m *Manager) IsCategoryDisabled(categoryRelPath string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalized := filepath.ToSlash(filepath.Clean(categoryRelPath))
	return m.disabledCategories[normalized]
}

// ResolvePath 将逻辑相对路径解析为具体的宿主机物理路径（第 1.1.4 节）。
// 格式为："{lib_id}:rel/path" 或纯相对路径（自动回退至主库）。
func (m *Manager) ResolvePath(logicalPath string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if logicalPath == "" {
		return "", errors.New("empty logical path")
	}

	parts := strings.SplitN(logicalPath, ":", 2)
	var targetLib *Library
	var relPath string

	if len(parts) == 2 {
		targetLib = m.libraries[parts[0]]
		relPath = parts[1]
	} else {
		if m.primaryID != "" {
			targetLib = m.libraries[m.primaryID]
		}
		relPath = parts[0]
	}

	if targetLib == nil {
		return "", fmt.Errorf("target library not found for path %s", logicalPath)
	}
	if !targetLib.IsOnline {
		return "", fmt.Errorf("target library %s is currently offline", targetLib.ID)
	}

	cleanRel := filepath.Clean(filepath.FromSlash(relPath))
	return filepath.Join(targetLib.RootPath, cleanRel), nil
}
