package gallery

import (
	"database/sql"
	"errors"
	"fmt"
	"omnihub/internal/storage"
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
	return openDB(galleryDir, false)
}

// OpenMetaDBReadOnly 只读打开已有数据库，不建目录、不迁移。
func OpenMetaDBReadOnly(galleryDir string) (*MetaDB, error) { return openDB(galleryDir, true) }

func openDB(galleryDir string, readOnly bool) (*MetaDB, error) {
	dbPath := filepath.Join(galleryDir, "gallery.db")
	db, err := storage.OpenSQLite(dbPath, readOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to open gallery.db: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	meta := &MetaDB{db: db}
	if !readOnly {
		if err := meta.migrate(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("gallery.db migration failed: %w", err)
		}

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

// GetThumbnail 读取最多 2MiB 的缩略图 BLOB，超限时不加载图像内容。
func (m *MetaDB) GetThumbnail(photoID string) ([]byte, error) {
	var data []byte
	err := m.db.QueryRow("SELECT CASE WHEN length(data) <= ? THEN data ELSE NULL END FROM thumbnails WHERE photo_id = ? AND size_type = 'm'", storage.MaxThumbnailReadBytes, photoID).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if data == nil {
		return nil, ErrImageCapacity
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

	_, err := m.db.Exec("UPDATE photos SET deleted_at = ? WHERE id = ? AND (deleted_at IS NULL) = ?", deletedAtStr, photoID, deleted)
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
	p.CreatedAt = parseStamp(createdStr)
	if deletedStr.Valid {
		t := parseStamp(deletedStr.String)
		p.DeletedAt = &t
	}

	return &p, nil
}

// PhotoFilter 选择照片；Color 为 -1 表示不限色系。软删除的照片始终排除。
type PhotoFilter struct {
	Color int
}

func (f PhotoFilter) clause() (string, []any) {
	if f.Color >= 0 {
		return " WHERE deleted_at IS NULL AND color_family = ?", []any{f.Color}
	}
	return " WHERE deleted_at IS NULL", nil
}

// ListPhotos 分页列出照片（按 created_at 倒序，同时间按 id），limit 上限 200。
func (m *MetaDB) ListPhotos(f PhotoFilter, limit, offset int) ([]*Photo, error) {
	if limit <= 0 || limit > 200 || offset < 0 {
		return nil, errors.New("invalid photo page")
	}
	where, args := f.clause()
	rows, err := m.db.Query(`SELECT id, filename, size_bytes, mime_type, width, height,
		volume, color_family, color_hex, is_favorite, created_at, deleted_at
		FROM photos`+where+" ORDER BY created_at DESC, id ASC LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var photos []*Photo
	for rows.Next() {
		var p Photo
		var favInt int
		var createdStr string
		var deletedStr sql.NullString
		if err := rows.Scan(
			&p.ID, &p.Filename, &p.SizeBytes, &p.MimeType, &p.Width, &p.Height,
			&p.Volume, &p.ColorFamily, &p.ColorHex, &favInt, &createdStr, &deletedStr,
		); err != nil {
			return nil, err
		}
		p.IsFavorite = favInt == 1
		p.CreatedAt = parseStamp(createdStr)
		photos = append(photos, &p)
	}
	return photos, rows.Err()
}

// CountPhotos 统计与 ListPhotos 相同筛选下的照片数。
func (m *MetaDB) CountPhotos(f PhotoFilter) (int, error) {
	where, args := f.clause()
	var count int
	err := m.db.QueryRow("SELECT COUNT(*) FROM photos"+where, args...).Scan(&count)
	return count, err
}

// ColorCounts 返回每个色系（0~9）的照片数。
func (m *MetaDB) ColorCounts() (map[int]int, error) {
	rows, err := m.db.Query("SELECT color_family, COUNT(*) FROM photos WHERE deleted_at IS NULL AND color_family >= 0 GROUP BY color_family")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[int]int)
	for rows.Next() {
		var color, n int
		if err := rows.Scan(&color, &n); err != nil {
			return nil, err
		}
		counts[color] = n
	}
	return counts, rows.Err()
}

// Ratios 按 ListPhotos 的顺序返回每张照片的高宽比 ×1000（瀑布流排版用）；尺寸缺失时按正方形。
func (m *MetaDB) Ratios(f PhotoFilter) ([]int, error) {
	where, args := f.clause()
	rows, err := m.db.Query("SELECT width, height FROM photos"+where+" ORDER BY created_at DESC, id ASC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ratios []int
	for rows.Next() {
		var w, h int
		if err := rows.Scan(&w, &h); err != nil {
			return nil, err
		}
		r := 1000
		if w > 0 && h > 0 {
			r = h * 1000 / w
		}
		ratios = append(ratios, r)
	}
	return ratios, rows.Err()
}

// FeedRow 是推荐用的照片概要。
type FeedRow struct {
	ID          string
	Width       int
	Height      int
	ColorFamily int
	ColorHex    string
}

// FeedRows 按 ListPhotos 的顺序返回全部照片的概要（推荐排序与多联放映用）。
func (m *MetaDB) FeedRows() ([]FeedRow, error) {
	rows, err := m.db.Query("SELECT id, COALESCE(width,0), COALESCE(height,0), COALESCE(color_family,-1), COALESCE(color_hex,'') FROM photos WHERE deleted_at IS NULL ORDER BY created_at DESC, id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeedRow
	for rows.Next() {
		var r FeedRow
		if err := rows.Scan(&r.ID, &r.Width, &r.Height, &r.ColorFamily, &r.ColorHex); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PhotosByID 按给定顺序返回照片，不存在的跳过。
func (m *MetaDB) PhotosByID(ids []string) ([]*Photo, error) {
	out := make([]*Photo, 0, len(ids))
	for _, id := range ids {
		p, err := m.GetPhoto(id)
		if err != nil {
			return nil, err
		}
		if p != nil && p.DeletedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}
