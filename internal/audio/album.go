// Package audio 实现全格式音频流化、专辑分集列表生成与播放断点记忆。
// 遵循 ARCHITECTURE.md 第 6.2 节规范。
package audio

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SupportedExtensions 定义支持的全格式音频后缀（第 6.2.1 节）。
var SupportedExtensions = map[string]bool{
	".m4a":  true,
	".mp3":  true,
	".flac": true,
	".wav":  true,
	".ogg":  true,
}

// Track 记录专辑内的单集音轨信息。
type Track struct {
	Index    int    `json:"index"`     // 自然排序序号 (1, 2, 3...)
	Title    string `json:"title"`     // 音轨标题（文件名去后缀）
	Filename string `json:"filename"`  // 实际文件名
	RelPath  string `json:"rel_path"`  // 相对专辑目录路径
	Ext      string `json:"ext"`       // 容器扩展名
}

// Album 记录目录映射出的专辑。
type Album struct {
	ID        string   `json:"id"`        // 专辑唯一 ID
	Title     string   `json:"title"`     // 专辑名称（父目录名）
	CoverPath string   `json:"cover_path"`// 封面相对或绝对路径
	Tracks    []*Track `json:"tracks"`    // 分集列表
}

// ScanAlbum 从指定物理目录扫描生成专辑对象及分集播放列表（第 6.2.2 节）。
func ScanAlbum(albumDir string) (*Album, error) {
	entries, err := os.ReadDir(albumDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read album dir: %w", err)
	}

	title := filepath.Base(albumDir)
	album := &Album{
		ID:     title,
		Title:  title,
		Tracks: make([]*Track, 0),
	}

	var trackFiles []string
	coverCandidate := ""

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		if SupportedExtensions[ext] {
			trackFiles = append(trackFiles, name)
		} else if strings.HasPrefix(strings.ToLower(name), "cover.") || strings.HasPrefix(strings.ToLower(name), "folder.") {
			coverCandidate = filepath.Join(albumDir, name)
		}
	}

	sort.Strings(trackFiles)
	for i, f := range trackFiles {
		stem := strings.TrimSuffix(f, filepath.Ext(f))
		album.Tracks = append(album.Tracks, &Track{
			Index:    i + 1,
			Title:    stem,
			Filename: f,
			RelPath:  f,
			Ext:      filepath.Ext(f),
		})
	}

	album.CoverPath = coverCandidate
	return album, nil
}

// ProgressDB 记录用户维度的音频断点（第 6.2.4 节）。
type ProgressDB struct {
	db *sql.DB
}

// OpenProgressDB 打开音频进度数据库。
func OpenProgressDB(dbPath string) (*ProgressDB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath))
	if err != nil {
		return nil, err
	}
	schema := `CREATE TABLE IF NOT EXISTS audio_progress (
		user_id INTEGER NOT NULL,
		audio_id TEXT NOT NULL,
		offset_sec REAL NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(user_id, audio_id)
	);`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &ProgressDB{db: db}, nil
}

// Close 关闭数据库。
func (p *ProgressDB) Close() error {
	return p.db.Close()
}

// SaveProgress 记录指定用户的音轨断点时间戳。
func (p *ProgressDB) SaveProgress(userID int64, audioID string, offsetSec float64) error {
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err := p.db.Exec(`
		INSERT INTO audio_progress (user_id, audio_id, offset_sec, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, audio_id) DO UPDATE SET
			offset_sec = excluded.offset_sec,
			updated_at = excluded.updated_at`,
		userID, audioID, offsetSec, nowStr,
	)
	return err
}

// GetProgress 读取用户的音频断点。
func (p *ProgressDB) GetProgress(userID int64, audioID string) (float64, error) {
	var offset float64
	err := p.db.QueryRow("SELECT offset_sec FROM audio_progress WHERE user_id = ? AND audio_id = ?", userID, audioID).Scan(&offset)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return offset, nil
}
