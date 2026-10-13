package shortvideo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"omnihub/internal/storage"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Storage 管理 shortvideo 专区的独立数据库（第 2.1 节）。
type Storage struct {
	metaDB  *sql.DB // meta.db
	thumbDB *sql.DB // thumbnails.db
	albumDB *sql.DB // albums.db
}

// OpenStorage 初始化短视频目录下的专属数据库。
func OpenStorage(shortvideoDir string) (*Storage, error) { return openStorage(shortvideoDir, false) }

// OpenStorageReadOnly 只读打开三个已有专区数据库，不建目录、不迁移。
func OpenStorageReadOnly(shortvideoDir string) (*Storage, error) {
	return openStorage(shortvideoDir, true)
}

func openStorage(shortvideoDir string, readOnly bool) (*Storage, error) {
	metaPath := filepath.Join(shortvideoDir, "meta.db")
	mDB, err := storage.OpenSQLite(metaPath, readOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to open meta.db: %w", err)
	}

	thumbPath := filepath.Join(shortvideoDir, "thumbnails.db")
	tDB, err := storage.OpenSQLite(thumbPath, readOnly)
	if err != nil {
		_ = mDB.Close()
		return nil, fmt.Errorf("failed to open thumbnails.db: %w", err)
	}

	albumPath := filepath.Join(shortvideoDir, "albums.db")
	aDB, err := storage.OpenSQLite(albumPath, readOnly)
	if err != nil {
		_ = mDB.Close()
		_ = tDB.Close()
		return nil, fmt.Errorf("failed to open albums.db: %w", err)
	}

	s := &Storage{
		metaDB:  mDB,
		thumbDB: tDB,
		albumDB: aDB,
	}

	if !readOnly {
		if err := s.migrate(); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("shortvideo db migration failed: %w", err)
		}

	}

	return s, nil
}

// Close 关闭数据库连接。
func (s *Storage) Close() error {
	var errs []error
	if s.metaDB != nil {
		if err := s.metaDB.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.thumbDB != nil {
		if err := s.thumbDB.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.albumDB != nil {
		if err := s.albumDB.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (s *Storage) migrate() error {
	metaSchema := `
	CREATE TABLE IF NOT EXISTS creators (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		platform TEXT NOT NULL,
		item_count INTEGER DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS posts (
		id TEXT PRIMARY KEY,
		creator_id TEXT NOT NULL,
		title TEXT NOT NULL,
		type TEXT NOT NULL,
		rel_path TEXT NOT NULL,
		created_at TEXT NOT NULL
	);`
	if _, err := s.metaDB.Exec(metaSchema); err != nil {
		return err
	}

	thumbSchema := `
	CREATE TABLE IF NOT EXISTS thumbnails (
		target_id TEXT PRIMARY KEY,
		data BLOB NOT NULL
	);`
	if _, err := s.thumbDB.Exec(thumbSchema); err != nil {
		return err
	}

	albumSchema := `
	CREATE TABLE IF NOT EXISTS album_images (
		post_id TEXT NOT NULL,
		order_index INTEGER NOT NULL,
		data BLOB NOT NULL,
		PRIMARY KEY (post_id, order_index)
	);
	CREATE TABLE IF NOT EXISTS album_image_hashes (
		post_id TEXT NOT NULL, order_index INTEGER NOT NULL,
		sha256 BLOB NOT NULL, size_bytes INTEGER NOT NULL,
		PRIMARY KEY(post_id, order_index)
	);`
	if _, err := s.albumDB.Exec(albumSchema); err != nil {
		return err
	}

	return nil
}

// SaveThumbnail 保存视频首帧或图集缩略图。
func (s *Storage) SaveThumbnail(targetID string, data []byte) error {
	if targetID == "" || len(data) == 0 {
		return errors.New("invalid thumbnail payload")
	}
	_, err := s.thumbDB.Exec(`
		INSERT INTO thumbnails (target_id, data) VALUES (?, ?)
		ON CONFLICT(target_id) DO UPDATE SET data = excluded.data`,
		targetID, data,
	)
	return err
}

// GetThumbnail 读取最多 2MiB 的指定目标缩略图，超限不加载 BLOB。
func (s *Storage) GetThumbnail(targetID string) ([]byte, error) {
	var data []byte
	err := s.thumbDB.QueryRow("SELECT CASE WHEN length(data) <= ? THEN data ELSE NULL END FROM thumbnails WHERE target_id = ?", storage.MaxThumbnailReadBytes, targetID).Scan(&data)
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

// SaveAlbumImage 保存图集单张图片。
func (s *Storage) SaveAlbumImage(postID string, orderIndex int, data []byte) error {
	if postID == "" || len(data) == 0 {
		return errors.New("invalid album image payload")
	}
	_, err := s.albumDB.Exec(`
		INSERT INTO album_images (post_id, order_index, data) VALUES (?, ?, ?)
		ON CONFLICT(post_id, order_index) DO UPDATE SET data = excluded.data`,
		postID, orderIndex, data,
	)
	return err
}

// GetAlbumImage 读取最多 32MiB 的图集图片，超限不加载 BLOB。
func (s *Storage) GetAlbumImage(postID string, orderIndex int) ([]byte, error) {
	var data []byte
	err := s.albumDB.QueryRow("SELECT CASE WHEN length(data) <= ? THEN data ELSE NULL END FROM album_images WHERE post_id = ? AND order_index = ?", storage.MaxImageReadBytes, postID, orderIndex).Scan(&data)
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

// MergeFromExternal 通过 SQLite 原生 ATTACH 语句跨库去重融合（第 2.3 节）。
func (s *Storage) MergeFromExternal(extThumbDBPath, extAlbumDBPath string) error {
	if extThumbDBPath != "" {
		if err := mergeDatabase(s.thumbDB, extThumbDBPath,
			`SELECT COUNT(*) FROM thumbnails t JOIN incoming.thumbnails i ON t.target_id=i.target_id WHERE t.data!=i.data`,
			`INSERT OR IGNORE INTO thumbnails(target_id,data) SELECT target_id,data FROM incoming.thumbnails`); err != nil {
			return fmt.Errorf("merge thumbnails: %w", err)
		}
	}

	if extAlbumDBPath != "" {
		if err := mergeDatabase(s.albumDB, extAlbumDBPath,
			`SELECT COUNT(*) FROM album_images t JOIN incoming.album_images i ON t.post_id=i.post_id AND t.order_index=i.order_index WHERE t.data!=i.data`,
			`INSERT OR IGNORE INTO album_images(post_id,order_index,data) SELECT post_id,order_index,data FROM incoming.album_images`); err != nil {
			return fmt.Errorf("merge albums: %w", err)
		}
	}

	return nil
}

func mergeDatabase(db *sql.DB, path, conflictQuery, mergeQuery string) (resultErr error) {
	uri, err := storage.ReadOnlyURI(path)
	if err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	// ATTACH/transaction/DETACH 必须使用同一连接，禁止依赖连接池恰好复用。
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS incoming", uri); err != nil {
		return err
	}
	defer func() {
		_, err := conn.ExecContext(ctx, "DETACH DATABASE incoming")
		resultErr = errors.Join(resultErr, err)
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	var conflicts int
	if err := tx.QueryRow(conflictQuery).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts != 0 {
		return errors.New("same asset key has different content; merge cancelled")
	}
	if _, err := tx.Exec(mergeQuery); err != nil {
		return err
	}
	return tx.Commit()
}

// ListAlbums 列出所有图集 post_id 与其图片数量。
func (s *Storage) ListAlbums() (map[string]int, error) {
	rows, err := s.albumDB.Query("SELECT post_id, count(*) FROM album_images GROUP BY post_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]int)
	for rows.Next() {
		var postID string
		var cnt int
		if err := rows.Scan(&postID, &cnt); err != nil {
			return nil, err
		}
		res[postID] = cnt
	}
	return res, rows.Err()
}

// UpsertCreator 插入或更新博主元数据。
func (s *Storage) UpsertCreator(c *Creator) error {
	if c.ID == "" {
		return errors.New("empty creator id")
	}
	_, err := s.metaDB.Exec(`
		INSERT INTO creators (id, name, platform, item_count) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, platform = excluded.platform, item_count = excluded.item_count`,
		c.ID, c.Name, c.Platform, c.ItemCount,
	)
	return err
}

// UpsertPost 插入或更新作品元数据。
func (s *Storage) UpsertPost(p *Post) error {
	if p.ID == "" {
		return errors.New("empty post id")
	}
	_, err := s.metaDB.Exec(`
		INSERT INTO posts (id, creator_id, title, type, rel_path, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET creator_id = excluded.creator_id, title = excluded.title, type = excluded.type, rel_path = excluded.rel_path`,
		p.ID, p.CreatorID, p.Title, p.Type, p.RelPath, p.CreatedAt.Format(time.RFC3339),
	)
	return err
}

// ListCreators 列出博主。
func (s *Storage) ListCreators(platform string) ([]*Creator, error) {
	query := "SELECT id, name, platform, item_count FROM creators"
	var args []interface{}
	if platform != "" {
		query += " WHERE platform = ?"
		args = append(args, platform)
	}
	rows, err := s.metaDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Creator
	for rows.Next() {
		var c Creator
		if err := rows.Scan(&c.ID, &c.Name, &c.Platform, &c.ItemCount); err != nil {
			return nil, err
		}
		list = append(list, &c)
	}
	return list, rows.Err()
}

// ListPosts 列出博主作品。
func (s *Storage) ListPosts(creatorID string) ([]*Post, error) {
	query := "SELECT id, creator_id, title, type, rel_path, created_at FROM posts WHERE creator_id = ?"
	rows, err := s.metaDB.Query(query, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Post
	for rows.Next() {
		var p Post
		var createdAtStr string
		if err := rows.Scan(&p.ID, &p.CreatorID, &p.Title, &p.Type, &p.RelPath, &createdAtStr); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		list = append(list, &p)
	}
	return list, rows.Err()
}
