package manga

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB 封装 manga 专区的独立数据库（第 7.1.1 节）。
type DB struct {
	db *sql.DB
}

// OpenDB 打开或创建 manga/manga.db。
func OpenDB(mangaDir string) (*DB, error) {
	if err := os.MkdirAll(mangaDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create manga dir: %w", err)
	}

	dbPath := filepath.Join(mangaDir, "manga.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	sqliteDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open manga.db: %w", err)
	}

	mDB := &DB{db: sqliteDB}
	if err := mDB.migrate(); err != nil {
		_ = sqliteDB.Close()
		return nil, fmt.Errorf("manga.db migration failed: %w", err)
	}

	return mDB, nil
}

// Close 关闭底层连接。
func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS mangas (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		author TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		tags_json TEXT NOT NULL DEFAULT '[]',
		chapter_count INTEGER NOT NULL DEFAULT 0,
		total_pages INTEGER NOT NULL DEFAULT 0,
		is_complete INTEGER NOT NULL DEFAULT 0,
		rel_path TEXT NOT NULL,
		custom_cover TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS manga_thumbnails (
		manga_id TEXT PRIMARY KEY,
		data BLOB NOT NULL
	);

	CREATE TABLE IF NOT EXISTS manga_progress (
		user_id INTEGER NOT NULL,
		manga_id TEXT NOT NULL,
		chapter_index INTEGER NOT NULL DEFAULT 1,
		page_index INTEGER NOT NULL DEFAULT 1,
		scroll_offset_ratio REAL NOT NULL DEFAULT 0.0,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(user_id, manga_id),
		FOREIGN KEY(manga_id) REFERENCES mangas(id) ON DELETE CASCADE
	);
	`
	_, err := d.db.Exec(schema)
	return err
}

// UpsertManga 插入或更新漫画信息。
func (d *DB) UpsertManga(m *Manga) error {
	if m.ID == "" || m.Title == "" {
		return errors.New("manga id and title cannot be empty")
	}

	tagsRaw, _ := json.Marshal(m.Tags)
	compInt := 0
	if m.IsComplete {
		compInt = 1
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`
		INSERT INTO mangas (
			id, title, author, description, tags_json, chapter_count,
			total_pages, is_complete, rel_path, custom_cover, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			author = excluded.author,
			description = excluded.description,
			tags_json = excluded.tags_json,
			chapter_count = excluded.chapter_count,
			total_pages = excluded.total_pages,
			is_complete = excluded.is_complete,
			rel_path = excluded.rel_path,
			updated_at = excluded.updated_at`,
		m.ID, m.Title, m.Author, m.Description, string(tagsRaw),
		m.ChapterCount, m.TotalPages, compInt, m.RelPath, m.CustomCover, nowStr,
	)
	return err
}

// SetCustomCover 用户在界面一键设为封面（第 7.2.3 节）。
func (d *DB) SetCustomCover(mangaID, coverEntry string) error {
	_, err := d.db.Exec("UPDATE mangas SET custom_cover = ? WHERE id = ?", coverEntry, mangaID)
	return err
}

// SaveProgress 记录条漫像素级阅读进度（第 7.3.2 节）。
func (d *DB) SaveProgress(p *MangaProgress) error {
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`
		INSERT INTO manga_progress (
			user_id, manga_id, chapter_index, page_index, scroll_offset_ratio, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, manga_id) DO UPDATE SET
			chapter_index = excluded.chapter_index,
			page_index = excluded.page_index,
			scroll_offset_ratio = excluded.scroll_offset_ratio,
			updated_at = excluded.updated_at`,
		p.UserID, p.MangaID, p.ChapterIndex, p.PageIndex, p.ScrollOffsetRatio, nowStr,
	)
	return err
}

// GetProgress 读取指定用户的阅读进度。
func (d *DB) GetProgress(userID int64, mangaID string) (*MangaProgress, error) {
	var p MangaProgress
	var updatedStr string
	err := d.db.QueryRow(`
		SELECT user_id, manga_id, chapter_index, page_index, scroll_offset_ratio, updated_at
		FROM manga_progress WHERE user_id = ? AND manga_id = ?`,
		userID, mangaID,
	).Scan(&p.UserID, &p.MangaID, &p.ChapterIndex, &p.PageIndex, &p.ScrollOffsetRatio, &updatedStr)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return &p, nil
}

// ListMangas 列出所有漫画。
func (d *DB) ListMangas() ([]*Manga, error) {
	rows, err := d.db.Query(`
		SELECT id, title, author, description, tags_json, chapter_count,
		       total_pages, is_complete, rel_path, custom_cover
		FROM mangas ORDER BY title ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Manga
	for rows.Next() {
		var m Manga
		var tagsStr string
		var compInt int
		if err := rows.Scan(
			&m.ID, &m.Title, &m.Author, &m.Description, &tagsStr,
			&m.ChapterCount, &m.TotalPages, &compInt, &m.RelPath, &m.CustomCover,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsStr), &m.Tags)
		m.IsComplete = compInt == 1
		list = append(list, &m)
	}
	return list, rows.Err()
}

// GetExistingIDs 获取数据库中所有已索引的漫画 ID。
func (d *DB) GetExistingIDs() (map[string]bool, error) {
	rows, err := d.db.Query("SELECT id FROM mangas")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			res[id] = true
		}
	}
	return res, rows.Err()
}

