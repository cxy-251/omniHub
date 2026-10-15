// Package shortvideo 维护单个资源库 media_library/shortvideo/ 下的自治数据库（ARCHITECTURE.md 第 2 节）。
//
// 目录约定：<平台>/<博主>/<作品>。作品是 .mp4 视频或装图片的图集目录；
// 与图集目录同名的 .mp3 是该图集的背景音乐。
//
// 所有 ID 都是相对 shortvideo/ 的斜杠路径：博主 "抖音/马羚薯"，作品 "抖音/马羚薯/xxx.mp4"。
// 数据库随资源库携带，因此不带库标识前缀。
//
// 三个数据库：
//   - index.db：博主与作品元数据；
//   - thumbnails.db：作品封面缩略图（视频首帧 / 图集首图，480px 宽 JPEG）；
//   - albums.db：入库的图集原图。
package shortvideo

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	"omnihub/internal/storage"
)

// SchemaVersion 写入每个数据库的 user_version；不同版本的旧库拒绝打开，避免误读 agy 阶段的遗留表。
const SchemaVersion = 2

// DatabaseFiles 是本模块在 shortvideo/ 下拥有的数据库文件名。
var DatabaseFiles = []string{"index.db", "thumbnails.db", "albums.db"}

// ThumbnailWidth 与相册热库统一采用 480px 宽缩略图。
const ThumbnailWidth = 480

// Store 持有一个资源库短视频目录下的三个数据库连接。
type Store struct {
	index    *sql.DB
	thumbs   *sql.DB
	albums   *sql.DB
	readOnly bool
}

const indexSchema = `
CREATE TABLE IF NOT EXISTS creators (
	id       TEXT PRIMARY KEY,
	platform TEXT NOT NULL,
	name     TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS posts (
	id          TEXT PRIMARY KEY,
	creator_id  TEXT NOT NULL,
	kind        TEXT NOT NULL CHECK (kind IN ('video', 'album')),
	title       TEXT NOT NULL,
	published   TEXT NOT NULL DEFAULT '',
	size        INTEGER NOT NULL,
	mtime       INTEGER NOT NULL,
	image_count INTEGER NOT NULL DEFAULT 0,
	music       TEXT NOT NULL DEFAULT '',
	on_disk     INTEGER NOT NULL DEFAULT 1,
	imported    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_posts_creator ON posts(creator_id, published DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_posts_order ON posts(published DESC, id DESC);`

const thumbSchema = `
CREATE TABLE IF NOT EXISTS thumbnails (
	post_id      TEXT PRIMARY KEY,
	source_size  INTEGER NOT NULL,
	source_mtime INTEGER NOT NULL,
	width        INTEGER NOT NULL,
	height       INTEGER NOT NULL,
	failed       INTEGER NOT NULL DEFAULT 0,
	data         BLOB NOT NULL
);`

const albumSchema = `
CREATE TABLE IF NOT EXISTS albums (
	post_id     TEXT PRIMARY KEY,
	image_count INTEGER NOT NULL,
	total_bytes INTEGER NOT NULL,
	imported_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS album_images (
	post_id     TEXT NOT NULL,
	order_index INTEGER NOT NULL,
	filename    TEXT NOT NULL,
	size_bytes  INTEGER NOT NULL,
	sha256      BLOB NOT NULL,
	data        BLOB NOT NULL,
	PRIMARY KEY (post_id, order_index)
);`

// Open 打开 dir（某资源库的 media_library/shortvideo）下的数据库。
// 可写模式下按需创建；只读模式要求三个库都已存在。
func Open(dir string, readOnly bool) (*Store, error) {
	s := &Store{readOnly: readOnly}
	targets := []struct {
		db     **sql.DB
		name   string
		schema string
	}{{&s.index, "index.db", indexSchema}, {&s.thumbs, "thumbnails.db", thumbSchema}, {&s.albums, "albums.db", albumSchema}}
	for _, t := range targets {
		db, err := storage.OpenSQLite(filepath.Join(dir, t.name), readOnly)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("open %s: %w", t.name, err), s.Close())
		}
		*t.db = db
		if err := prepare(db, t.schema, readOnly); err != nil {
			return nil, errors.Join(fmt.Errorf("%s: %w", t.name, err), s.Close())
		}
	}
	return s, nil
}

// prepare 校验版本并建表。空库（user_version=0 且无表）视为新库。
func prepare(db *sql.DB, schema string, readOnly bool) error {
	var version, tables int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == SchemaVersion {
		return nil
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&tables); err != nil {
		return err
	}
	if version != 0 || tables != 0 {
		return fmt.Errorf("unsupported schema version %d; move the old database aside", version)
	}
	if readOnly {
		return errors.New("database not initialized")
	}
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion))
	return err
}

// ReadOnly 报告该库是否以只读方式打开。
func (s *Store) ReadOnly() bool { return s.readOnly }

// Close 关闭全部连接，可重复调用。
func (s *Store) Close() error {
	var errs []error
	for _, db := range []**sql.DB{&s.index, &s.thumbs, &s.albums} {
		if *db != nil {
			errs = append(errs, (*db).Close())
			*db = nil
		}
	}
	return errors.Join(errs...)
}
