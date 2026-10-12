package gallery

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// MetaDB 负责管理相册热元数据库 gallery.db（第 3.1.2 节、第 3.2.2 节）。
type MetaDB struct {
	db *sql.DB
}

// OpenMetaDB 打开或创建指定目录下的 gallery.db。
func OpenMetaDB(galleryDir string) (*MetaDB, error) {
	if err := os.MkdirAll(galleryDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create gallery dir: %w", err)
	}

	dbPath := filepath.Join(galleryDir, "gallery.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open gallery.db: %w", err)
	}

	meta := &MetaDB{db: db}
	if err := meta.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("gallery.db migration failed: %w", err)
	}

	return meta, nil
}

// Close 关闭数据库连接。
func (m *MetaDB) Close() error {
	return m.db.Close()
}

func (m *MetaDB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS photos (
		id TEXT PRIMARY KEY,
		filename TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		mime_type TEXT NOT NULL,
		width INTEGER NOT NULL,
		height INTEGER NOT NULL,
		volume TEXT NOT NULL,
		color_family INTEGER NOT NULL DEFAULT 0,
		color_hex TEXT NOT NULL DEFAULT '',
		is_favorite INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		deleted_at TEXT
	);

	CREATE TABLE IF NOT EXISTS thumbnails (
		photo_id TEXT NOT NULL,
		size_type TEXT NOT NULL DEFAULT 'm',
		width INTEGER NOT NULL,
		height INTEGER NOT NULL,
		data BLOB NOT NULL,
		PRIMARY KEY (photo_id, size_type),
		FOREIGN KEY(photo_id) REFERENCES photos(id) ON DELETE CASCADE
	);
	`
	_, err := m.db.Exec(schema)
	return err
}

// UpsertPhoto 插入或更新照片元数据。
func (m *MetaDB) UpsertPhoto(p *Photo) error {
	if p.ID == "" {
		return errors.New("photo id cannot be empty")
	}

	var deletedAtStr *string
	if p.DeletedAt != nil {
		s := p.DeletedAt.UTC().Format(time.RFC3339)
		deletedAtStr = &s
	}

	favInt := 0
	if p.IsFavorite {
		favInt = 1
	}

	_, err := m.db.Exec(`
		INSERT INTO photos (
			id, filename, size_bytes, mime_type, width, height,
			volume, color_family, color_hex, is_favorite, created_at, deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			filename = excluded.filename,
			size_bytes = excluded.size_bytes,
			mime_type = excluded.mime_type,
			width = excluded.width,
			height = excluded.height,
			volume = excluded.volume,
			color_family = excluded.color_family,
			color_hex = excluded.color_hex,
			is_favorite = excluded.is_favorite,
			deleted_at = excluded.deleted_at`,
		p.ID, p.Filename, p.SizeBytes, p.MimeType, p.Width, p.Height,
		p.Volume, p.ColorFamily, p.ColorHex, favInt, p.CreatedAt.UTC().Format(time.RFC3339), deletedAtStr,
	)
	return err
}

// SaveThumbnail 保存 480px 缩略图数据。
func (m *MetaDB) SaveThumbnail(t *Thumbnail) error {
	if t.PhotoID == "" || len(t.Data) == 0 {
		return errors.New("invalid thumbnail payload")
	}

	sizeType := t.SizeType
	if sizeType == "" {
		sizeType = "m"
	}

	_, err := m.db.Exec(`
		INSERT INTO thumbnails (photo_id, size_type, width, height, data)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(photo_id, size_type) DO UPDATE SET
			width = excluded.width,
			height = excluded.height,
			data = excluded.data`,
		t.PhotoID, sizeType, t.Width, t.Height, t.Data,
	)
	return err
}

// GetThumbnail 读取指定照片的缩略图 BLOB。
func (m *MetaDB) GetThumbnail(photoID string) ([]byte, error) {
	var data []byte
	err := m.db.QueryRow("SELECT data FROM thumbnails WHERE photo_id = ? AND size_type = 'm'", photoID).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// SoftDelete 标记照片为软删除（废纸篓打标，第 3.3.3 节）。
func (m *MetaDB) SoftDelete(photoID string, deleted bool) error {
	var deletedAtStr *string
	if deleted {
		s := time.Now().UTC().Format(time.RFC3339)
		deletedAtStr = &s
	}

	_, err := m.db.Exec("UPDATE photos SET deleted_at = ? WHERE id = ?", deletedAtStr, photoID)
	return err
}

// GetPhoto 获取单张照片元数据。
func (m *MetaDB) GetPhoto(photoID string) (*Photo, error) {
	var p Photo
	var favInt int
	var createdStr string
	var deletedStr sql.NullString

	err := m.db.QueryRow(`
		SELECT id, filename, size_bytes, mime_type, width, height,
		       volume, color_family, color_hex, is_favorite, created_at, deleted_at
		FROM photos WHERE id = ?`, photoID,
	).Scan(
		&p.ID, &p.Filename, &p.SizeBytes, &p.MimeType, &p.Width, &p.Height,
		&p.Volume, &p.ColorFamily, &p.ColorHex, &favInt, &createdStr, &deletedStr,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	p.IsFavorite = favInt == 1
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	if deletedStr.Valid {
		t, _ := time.Parse(time.RFC3339, deletedStr.String)
		p.DeletedAt = &t
	}

	return &p, nil
}
