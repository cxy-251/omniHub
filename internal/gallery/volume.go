package gallery

import (
	"database/sql"
	"errors"
	"fmt"
	"omnihub/internal/storage"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// VolumeManager 负责冷原图分卷的写满封卷与只读句柄分发（第 3.1.2 节、第 3.3.1 节）。
type VolumeManager struct {
	mu           sync.RWMutex
	originalsDir string
	readHandles  map[string]*sql.DB // 只读句柄缓存 (附加 mode=ro)
	activeVol    string             // 当前可追加写入的分卷文件名
	activeDB     *sql.DB            // 写入连接
}

// NewVolumeManager 初始化分卷管理器。
func NewVolumeManager(originalsDir string) (*VolumeManager, error) {
	if err := os.MkdirAll(originalsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create originals dir: %w", err)
	}

	vm := &VolumeManager{
		originalsDir: originalsDir,
		readHandles:  make(map[string]*sql.DB),
	}

	if err := vm.locateOrInitActiveVolume(); err != nil {
		return nil, err
	}

	return vm, nil
}

// Close 关闭所有打开的写入与只读分卷句柄。
func (vm *VolumeManager) Close() error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	var firstErr error
	if vm.activeDB != nil {
		if err := vm.activeDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	for _, db := range vm.readHandles {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// locateOrInitActiveVolume 扫描并寻找当前未满 4GB 的最新分卷，若均已满则新建。
func (vm *VolumeManager) locateOrInitActiveVolume() error {
	entries, err := os.ReadDir(vm.originalsDir)
	if err != nil {
		return err
	}

	var volFiles []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "originals_vol") && strings.HasSuffix(name, ".db") {
			volFiles = append(volFiles, name)
		}
	}

	sort.Strings(volFiles)

	var targetVol string
	for _, vol := range volFiles {
		fi, err := os.Stat(filepath.Join(vm.originalsDir, vol))
		if err == nil && fi.Size() < MaxVolumeSizeBytes {
			targetVol = vol
			break
		}
	}

	if targetVol == "" {
		nextIdx := 1
		if len(volFiles) > 0 {
			last := volFiles[len(volFiles)-1]
			// 从 originals_vol08.db 提取编号 8
			numPart := strings.TrimPrefix(last, "originals_vol")
			numPart = strings.TrimSuffix(numPart, ".db")
			if n, err := strconv.Atoi(numPart); err == nil {
				nextIdx = n + 1
			}
		}
		targetVol = fmt.Sprintf("originals_vol%02d.db", nextIdx)
	}

	return vm.switchActiveVolume(targetVol)
}

func (vm *VolumeManager) switchActiveVolume(volName string) error {
	if vm.activeDB != nil {
		_ = vm.activeDB.Close()
		vm.activeDB = nil
	}

	dbPath := filepath.Join(vm.originalsDir, volName)
	db, err := storage.OpenSQLite(dbPath, false)
	if err != nil {
		return fmt.Errorf("failed to open active volume %s: %w", volName, err)
	}

	// 严格 schema：表仅 (id, data) 两列，严禁新增其他列（第 3.2.1 节）
	schema := `CREATE TABLE IF NOT EXISTS originals (
		id TEXT PRIMARY KEY,
		data BLOB NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to init originals table: %w", err)
	}

	vm.activeVol = volName
	vm.activeDB = db
	return nil
}

// WriteOriginal 将原图二进制写入当前活动分卷，超额 4GB 自动封卷顺延。
func (vm *VolumeManager) WriteOriginal(id string, data []byte) (string, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// 写入当前活动卷
	_, err := vm.activeDB.Exec("INSERT OR IGNORE INTO originals (id, data) VALUES (?, ?)", id, data)
	if err != nil {
		return "", fmt.Errorf("failed to write original %s: %w", id, err)
	}

	writtenVol := vm.activeVol

	// 检查当前卷大小是否超额 4GB，若达到则封卷顺延创建新卷（第 3.3.2 节）
	fi, err := os.Stat(filepath.Join(vm.originalsDir, vm.activeVol))
	if err == nil && fi.Size() >= MaxVolumeSizeBytes {
		numPart := strings.TrimPrefix(vm.activeVol, "originals_vol")
		numPart = strings.TrimSuffix(numPart, ".db")
		currIdx, _ := strconv.Atoi(numPart)
		nextVol := fmt.Sprintf("originals_vol%02d.db", currIdx+1)
		_ = vm.switchActiveVolume(nextVol)
	}

	return writtenVol, nil
}

// ReadOriginal 从指定分卷只读读取原图（严格以 mode=ro 挂载，第 3.1.2 节）。
func (vm *VolumeManager) ReadOriginal(volumeName, id string) ([]byte, error) {
	if !volumeNamePattern.MatchString(volumeName) {
		return nil, errors.New("invalid originals volume name")
	}
	vm.mu.Lock()
	db, ok := vm.readHandles[volumeName]
	if !ok {
		dbPath := filepath.Join(vm.originalsDir, volumeName)
		if _, err := os.Stat(dbPath); err != nil {
			vm.mu.Unlock()
			return nil, fmt.Errorf("volume file not found: %s", volumeName)
		}

		// 只读模式挂载 (mode=ro)
		var err error
		db, err = storage.OpenSQLite(dbPath, true)
		if err != nil {
			vm.mu.Unlock()
			return nil, fmt.Errorf("failed to open read-only volume %s: %w", volumeName, err)
		}
		vm.readHandles[volumeName] = db
	}
	vm.mu.Unlock()

	var blob []byte
	err := db.QueryRow("SELECT data FROM originals WHERE id = ?", id).Scan(&blob)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read original from %s: %w", volumeName, err)
	}

	return blob, nil
}

// ReadOriginalDirect 纯只读方式直接从分卷读取原图（严格以 mode=ro 打开，零写操作零建表，第 3.1.2 节）。
// 单次原图读取限制 32MiB，超限时不加载 BLOB 内容。
func ReadOriginalDirect(originalsDir, volumeName, id string) (data []byte, result error) {
	if !volumeNamePattern.MatchString(volumeName) {
		return nil, errors.New("invalid originals volume name")
	}
	dbPath := filepath.Join(originalsDir, volumeName)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("volume file not found: %s", volumeName)
	}

	db, err := storage.OpenSQLite(dbPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to open read-only volume %s: %w", volumeName, err)
	}
	defer func() { result = errors.Join(result, db.Close()) }()

	var blob []byte
	err = db.QueryRow("SELECT CASE WHEN length(data) <= ? THEN data ELSE NULL END FROM originals WHERE id = ?", 32<<20, id).Scan(&blob)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read original from %s: %w", volumeName, err)
	}
	if blob == nil {
		return nil, ErrImageCapacity
	}
	return blob, nil
}

var volumeNamePattern = regexp.MustCompile(`^originals_vol[0-9]+\.db$`)
