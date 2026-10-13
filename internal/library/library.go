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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// LibraryMarkerFile 是放置在资源库根目录下的物理库标识文件名。
const LibraryMarkerFile = "omnilibrary.json"

// DataDirName 是主媒体库根目录下存放系统数据库与状态的隐藏目录名。
const DataDirName = ".omni_data"

// SameRoot 比较规范化资源库路径，Windows 上按不区分大小写的宿主路径语义处理。
// 不访问磁盘，因此可用于离线库与只读权限查询。
func SameRoot(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	left, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	right, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// StandardDirs 定义统一的资源库骨架子目录清单。
var StandardDirs = []string{
	// 独立游戏与模拟器分类
	"standalone_games/rpg_games",
	"standalone_games/renpy_games",
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

// ReadMarker 有界读取并验证物理库标识，拒绝超大文件与逻辑路径分隔符。
func ReadMarker(root string) (marker *Marker, result error) {
	f, err := os.Open(filepath.Join(root, LibraryMarkerFile))
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<10 {
		return nil, errors.New("library marker exceeds size limit")
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid library marker: %w", err)
	}
	if m.ID == "" || len(m.ID) > 128 || strings.ContainsAny(m.ID, "/\\:\x00") || len(m.Name) > 256 {
		return nil, errors.New("invalid library identity or name")
	}
	return &m, nil
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
	// ReadOnly 表示挂载不创建标识、目录或数据库，也不修改资产。
	ReadOnly bool `json:"read_only"`
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

// SetPrimary 设置已挂载库的主库标记；空标识清除主库，不迁移或修改磁盘状态。
// 仅供已经完成全局状态迁移的生命周期控制器调用。
func (m *Manager) SetPrimary(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != "" && m.libraries[id] == nil {
		return errors.New("primary library is not mounted")
	}
	m.primaryID = id
	for _, lib := range m.libraries {
		lib.IsPrimary = lib.ID == id
	}
	return nil
}

// generateUUID 生成 16 字节十六进制随机字符串作为资源库唯一标识。
func generateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate library ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// EnsureMarker 检查并确保指定根目录下存在物理标识文件，不存在则自动生成。
func EnsureMarker(rootPath string, defaultName string) (*Marker, error) {
	markerPath := filepath.Join(rootPath, LibraryMarkerFile)
	m, err := ReadMarker(rootPath)
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read library marker: %w", err)
	}

	if defaultName == "" {
		defaultName = filepath.Base(rootPath)
	}

	id, err := generateUUID()
	if err != nil {
		return nil, err
	}
	m = &Marker{
		ID:   id,
		Name: defaultName,
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal library marker: %w", err)
	}

	// 独占创建，避免并发挂载覆盖另一进程刚生成的稳定标识。
	f, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		// 已有标识可能是悬空链接或其他进程正在写入，绝不递归生成新 ID。
		return ReadMarker(rootPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to write library marker: %w", err)
	}
	_, writeErr := f.Write(raw)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return nil, fmt.Errorf("failed to persist library marker: %w", err)
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
	return m.MountLibrary(rootPath, isPrimary, false)
}

// MountLibrary 挂载资源库；只读库必须已有合法标识，绝不补建目录或状态文件。
func (m *Manager) MountLibrary(rootPath string, isPrimary, readOnly bool) (*Library, error) {
	return m.MountKnownLibrary(rootPath, isPrimary, readOnly, nil)
}

// MountKnownLibrary 使用持久化标识恢复离线库；在线介质必须与已记录标识一致。
// known 为 nil 时兼容首次挂载，调用方应保存返回的标识以便下次恢复。
func (m *Manager) MountKnownLibrary(rootPath string, isPrimary, readOnly bool, known *Marker) (*Library, error) {
	if rootPath == "" {
		return nil, errors.New("library root path cannot be empty")
	}

	cleanPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("resolve library root: %w", err)
	}

	// 检查目录是否存在与在线状态（第 1.4.3 节）
	info, err := os.Stat(cleanPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect library root: %w", err)
	}
	if err == nil && !info.IsDir() {
		return nil, errors.New("library root must be a directory")
	}
	isOnline := err == nil && info.IsDir()

	var marker *Marker
	if isOnline {
		if known != nil && known.ID != "" {
			current, err := ReadMarker(cleanPath)
			if err != nil {
				return nil, fmt.Errorf("read known library identity: %w", err)
			}
			if current.ID != known.ID {
				return nil, errors.New("mounted medium does not match configured library identity")
			}
		}
		// 在线状态下确保标识文件与骨架存在
		if readOnly {
			marker, err = ReadMarker(cleanPath)
		} else {
			marker, err = EnsureMarker(cleanPath, "")
		}
		if err != nil {
			return nil, fmt.Errorf("failed to verify marker in %s: %w", cleanPath, err)
		}
		if !readOnly {
			if err := EnsureSkeleton(cleanPath); err != nil {
				return nil, fmt.Errorf("failed to verify skeleton in %s: %w", cleanPath, err)
			}
		}
	} else {
		// 离线状态下分配临时标记
		marker = &Marker{
			ID:   "offline-" + cleanPath,
			Name: filepath.Base(cleanPath),
		}
		if known != nil && known.ID != "" {
			copy := *known
			marker = &copy
		}
	}

	lib := &Library{
		ID:        marker.ID,
		Name:      marker.Name,
		RootPath:  cleanPath,
		IsPrimary: isPrimary,
		IsOnline:  isOnline,
		ReadOnly:  readOnly,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing := m.libraries[lib.ID]; existing != nil && !SameRoot(existing.RootPath, cleanPath) {
		return nil, errors.New("library ID already mounted at another root")
	}
	if isPrimary || m.primaryID == "" {
		// 若为主库，确保 .omni_data 目录存在（第 1.3.1 节）
		if isOnline && !readOnly {
			dataDir := filepath.Join(cleanPath, DataDirName)
			if err := os.MkdirAll(dataDir, 0755); err != nil {
				return nil, fmt.Errorf("create primary state directory: %w", err)
			}
		}
		for _, existing := range m.libraries {
			existing.IsPrimary = false
		}
		m.primaryID = lib.ID
	}
	lib.IsPrimary = m.primaryID == lib.ID
	m.libraries[lib.ID] = lib

	snapshot := *lib
	return &snapshot, nil
}

// RemoveLibrary 移除指定资源库映射（不删除任何物理文件）。
func (m *Manager) RemoveLibrary(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.libraries, id)
	if m.primaryID == id {
		m.primaryID = ""
		// 主库承载全局状态，移除映射不能暗中切换账号数据库。
		// 新主库由显式迁移流程选定。
	}
}

// GetPrimary 获取当前主媒体库实例。若未配置任何库返回 nil（满足 0 库容错）。
func (m *Manager) GetPrimary() *Library {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.primaryID == "" {
		return nil
	}
	lib := m.libraries[m.primaryID]
	if lib == nil {
		return nil
	}
	snapshot := *lib
	return &snapshot
}

// ListLibraries 返回当前所有已挂载的资源库列表。
func (m *Manager) ListLibraries() []*Library {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*Library, 0, len(m.libraries))
	for _, lib := range m.libraries {
		snapshot := *lib
		list = append(list, &snapshot)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

// IsReadOnly 返回已挂载物理根路径是否只读，未挂载时返回 false。
func (m *Manager) IsReadOnly(root string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, lib := range m.libraries {
		if SameRoot(lib.RootPath, root) {
			return lib.ReadOnly
		}
	}
	return false
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
	return m.resolvePathLocked(logicalPath)
}

// resolvePathLocked 在调用方持有读锁时解析路径，确保分类检查使用同一挂载快照。
func (m *Manager) resolvePathLocked(logicalPath string) (string, error) {

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
	if relPath == "" || !filepath.IsLocal(cleanRel) || strings.ContainsAny(relPath, "\\\x00") || strings.Contains(relPath, ":") {
		return "", errors.New("resource path must be relative and remain inside its library")
	}
	root, err := filepath.EvalSymlinks(targetLib.RootPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve library root: %w", err)
	}
	target := filepath.Join(root, cleanRel)
	// 未创建的文件也允许解析，但其最近的已有父目录不能通过符号链接越界。
	ancestor := target
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			rel, err := filepath.Rel(root, resolved)
			if err != nil || !filepath.IsLocal(rel) {
				return "", errors.New("resource symlink escapes library root")
			}
			remaining, err := filepath.Rel(ancestor, target)
			if err != nil {
				return "", fmt.Errorf("resolve remaining resource path: %w", err)
			}
			return filepath.Join(resolved, remaining), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("failed to resolve resource path: %w", err)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", errors.New("resource path has no accessible parent")
		}
		ancestor = parent
	}
}

// ResolveCategoryPath 限制资源在指定分类目录内，防止低权限媒体接口读取库内账号或其他分类。
// allowedDirs 使用库根目录下的逻辑相对目录；库内跨分类符号链接同样被拒绝。
func (m *Manager) ResolveCategoryPath(logicalPath string, allowedDirs ...string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	path, err := m.resolvePathLocked(logicalPath)
	if err != nil {
		return "", err
	}
	id := m.primaryID
	if parts := strings.SplitN(logicalPath, ":", 2); len(parts) == 2 {
		id = parts[0]
	}
	lib := m.libraries[id]
	if lib == nil {
		return "", errors.New("library no longer mounted")
	}
	root, err := filepath.EvalSymlinks(lib.RootPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	for _, dir := range allowedDirs {
		dir = filepath.Clean(filepath.FromSlash(dir))
		if m.disabledCategories[filepath.ToSlash(dir)] {
			continue
		}
		if rel == dir || strings.HasPrefix(rel, dir+string(filepath.Separator)) {
			return path, nil
		}
	}
	return "", errors.New("resource outside permitted category or category disabled")
}
