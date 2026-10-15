// Package audio 实现全格式音频流化、专辑分集列表生成与播放断点记忆。
// 遵循 ARCHITECTURE.md 第 6.2 节规范。
package audio

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"omnihub/internal/ordering"
	"omnihub/internal/storage"

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

// ErrInvalidProgress 表示账号、音轨标识或断点数值不符合存储边界。
var ErrInvalidProgress = errors.New("invalid audio progress")

// FileExtensions 返回支持的后缀副本，供目录索引与流播放共享同一格式清单。
func FileExtensions() []string {
	result := make([]string, 0, len(SupportedExtensions))
	for ext := range SupportedExtensions {
		result = append(result, ext)
	}
	sort.Strings(result)
	return result
}

// Track 记录专辑内的单集音轨信息。
type Track struct {
	Index    int    `json:"index"`    // 自然排序序号 (1, 2, 3...)
	Title    string `json:"title"`    // 音轨标题（文件名去后缀）
	Filename string `json:"filename"` // 实际文件名
	RelPath  string `json:"rel_path"` // 相对专辑目录路径
	Ext      string `json:"ext"`      // 容器扩展名
}

// Album 记录目录映射出的专辑。
type Album struct {
	ID          string   `json:"id"`         // 专辑唯一 ID
	Title       string   `json:"title"`      // 专辑名称（父目录名）
	CoverPath   string   `json:"cover_path"` // 目录内相对路径；服务接口转换为库 UUID 逻辑路径
	Tracks      []*Track `json:"tracks"`     // 分集列表
	TrackCount  int      `json:"track_count"`
	LibraryID   string   `json:"library_id,omitempty"`
	LibraryName string   `json:"library_name,omitempty"`
}

// ScanAlbum 从指定物理目录扫描生成专辑对象及分集播放列表（第 6.2.2 节）。
func ScanAlbum(albumDir string) (album *Album, result error) {
	root, err := os.OpenRoot(albumDir)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := f.ReadDir(20001)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, fmt.Errorf("failed to read album dir: %w", err)
	}
	if len(entries) > 20000 {
		return nil, errors.New("album exceeds track capacity")
	}

	title := filepath.Base(albumDir)
	album = &Album{
		ID:     title,
		Title:  title,
		Tracks: make([]*Track, 0),
	}

	var trackFiles []string
	coverCandidate := ""

	for _, entry := range entries {
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		if SupportedExtensions[ext] {
			trackFiles = append(trackFiles, name)
		} else if (ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp") && (strings.HasPrefix(strings.ToLower(name), "cover.") || strings.HasPrefix(strings.ToLower(name), "folder.")) {
			coverCandidate = name
		}
	}

	sort.Slice(trackFiles, func(i, j int) bool { return ordering.NaturalLess(trackFiles[i], trackFiles[j]) })
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
	album.TrackCount = len(album.Tracks)
	return album, nil
}

// ProgressDB 记录用户维度的音频断点（第 6.2.4 节）。
type ProgressDB struct {
	db *sql.DB
}

// OpenProgressDB 打开音频进度数据库。
func OpenProgressDB(dbPath string) (*ProgressDB, error) {
	db, err := storage.OpenSQLite(dbPath, false)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
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
	// 后加的时长列：列表据此显示进度条与「已听完」。
	var hasDuration int
	if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('audio_progress') WHERE name='duration_sec'").Scan(&hasDuration); err != nil {
		_ = db.Close()
		return nil, err
	}
	if hasDuration == 0 {
		if _, err := db.Exec("ALTER TABLE audio_progress ADD COLUMN duration_sec REAL NOT NULL DEFAULT 0"); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &ProgressDB{db: db}, nil
}

// Close 关闭数据库。
func (p *ProgressDB) Close() error {
	return p.db.Close()
}

func validSeconds(v float64) bool {
	return v >= 0 && v <= 31536000 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

// SaveProgress 记录指定用户的音轨断点与时长（时长未知时传 0，保留已有值）。
func (p *ProgressDB) SaveProgress(userID int64, audioID string, offsetSec, durationSec float64) error {
	if userID <= 0 || audioID == "" || len(audioID) > 4096 || !validSeconds(offsetSec) || !validSeconds(durationSec) {
		return ErrInvalidProgress
	}
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err := p.db.Exec(`
		INSERT INTO audio_progress (user_id, audio_id, offset_sec, duration_sec, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, audio_id) DO UPDATE SET
			offset_sec = excluded.offset_sec,
			duration_sec = CASE WHEN excluded.duration_sec > 0 THEN excluded.duration_sec ELSE audio_progress.duration_sec END,
			updated_at = excluded.updated_at WHERE audio_progress.offset_sec != excluded.offset_sec`,
		userID, audioID, offsetSec, durationSec, nowStr,
	)
	return err
}

// Progress 是一条断点记录。
type Progress struct {
	AudioID   string  `json:"audio_id"`
	Offset    float64 `json:"offset"`
	Duration  float64 `json:"duration"`
	UpdatedAt string  `json:"updated_at"`
}

// ProgressFor 批量读取一组音轨的断点（最多 200 个）。
func (p *ProgressDB) ProgressFor(userID int64, audioIDs []string) (map[string]Progress, error) {
	out := map[string]Progress{}
	if userID <= 0 || len(audioIDs) == 0 {
		return out, nil
	}
	if len(audioIDs) > 200 {
		return nil, ErrInvalidProgress
	}
	query := "SELECT audio_id, offset_sec, duration_sec, updated_at FROM audio_progress WHERE user_id=? AND audio_id IN (?" + strings.Repeat(",?", len(audioIDs)-1) + ")"
	args := []any{userID}
	for _, id := range audioIDs {
		args = append(args, id)
	}
	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pr Progress
		if err := rows.Scan(&pr.AudioID, &pr.Offset, &pr.Duration, &pr.UpdatedAt); err != nil {
			return nil, err
		}
		out[pr.AudioID] = pr
	}
	return out, rows.Err()
}

// Recent 按最近更新时间返回用户的断点（含已听完的），最多 limit 条。
func (p *ProgressDB) Recent(userID int64, limit int) ([]Progress, error) {
	if userID <= 0 {
		return nil, nil
	}
	rows, err := p.db.Query("SELECT audio_id, offset_sec, duration_sec, updated_at FROM audio_progress WHERE user_id=? ORDER BY updated_at DESC LIMIT ?", userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Progress
	for rows.Next() {
		var pr Progress
		if err := rows.Scan(&pr.AudioID, &pr.Offset, &pr.Duration, &pr.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
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
