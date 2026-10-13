package shortvideo

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Storage 管理 shortvideo 专区的独立数据库（第 2.1 节）。
type Storage struct {
	thumbDB *sql.DB // thumbnails.db
	albumDB *sql.DB // albums.db
}

// OpenStorage 初始化短视频目录下的专属数据库。
func OpenStorage(shortvideoDir string) (*Storage, error) {
	if err := os.MkdirAll(shortvideoDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create shortvideo dir: %w", err)
	}

	thumbPath := filepath.Join(shortvideoDir, "thumbnails.db")
	tDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", thumbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open thumbnails.db: %w", err)
	}

	albumPath := filepath.Join(shortvideoDir, "albums.db")
	aDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", albumPath))
	if err != nil {
		_ = tDB.Close()
		return nil, fmt.Errorf("failed to open albums.db: %w", err)
	}

	s := &Storage{
		thumbDB: tDB,
		albumDB: aDB,
	}

	if err := s.migrate(); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("shortvideo db migration failed: %w", err)
	}

	return s, nil
}

// Close 关闭数据库连接。
func (s *Storage) Close() error {
	var err1, err2 error
	if s.thumbDB != nil {
		err1 = s.thumbDB.Close()
	}
	if s.albumDB != nil {
		err2 = s.albumDB.Close()
	}
	if err1 != nil {
		return err1
	}
	return err2
}

func (s *Storage) migrate() error {
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

// GetThumbnail 读取指定目标缩略图。
func (s *Storage) GetThumbnail(targetID string) ([]byte, error) {
	var data []byte
	err := s.thumbDB.QueryRow("SELECT data FROM thumbnails WHERE target_id = ?", targetID).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
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

// GetAlbumImage 读取图集指定序号的图片。
func (s *Storage) GetAlbumImage(postID string, orderIndex int) ([]byte, error) {
	var data []byte
	err := s.albumDB.QueryRow("SELECT data FROM album_images WHERE post_id = ? AND order_index = ?", postID, orderIndex).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// MergeFromExternal 通过 SQLite 原生 ATTACH 语句跨库去重融合（第 2.3 节）。
func (s *Storage) MergeFromExternal(extThumbDBPath, extAlbumDBPath string) error {
	if extThumbDBPath != "" {
		attachSQL := fmt.Sprintf("ATTACH DATABASE '%s' AS ext_thumb", extThumbDBPath)
		if _, err := s.thumbDB.Exec(attachSQL); err == nil {
			_, _ = s.thumbDB.Exec("INSERT OR IGNORE INTO thumbnails SELECT * FROM ext_thumb.thumbnails")
			_, _ = s.thumbDB.Exec("DETACH DATABASE ext_thumb")
		}
	}

	if extAlbumDBPath != "" {
		attachSQL := fmt.Sprintf("ATTACH DATABASE '%s' AS ext_album", extAlbumDBPath)
		if _, err := s.albumDB.Exec(attachSQL); err == nil {
			_, _ = s.albumDB.Exec("INSERT OR IGNORE INTO album_images SELECT * FROM ext_album.album_images")
			_, _ = s.albumDB.Exec("DETACH DATABASE ext_album")
		}
	}

	return nil
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

