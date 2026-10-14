// Package userstate persists per-account state independently of read-only media.
package userstate

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"omnihub/internal/manga"
	"omnihub/internal/storage"
)

// ErrInvalidProgress reports an account, logical identity or position outside its bounds.
var ErrInvalidProgress = errors.New("invalid reading progress")

// DB owns the fixed global user_state.db connection pool; it never opens media files.
type DB struct{ db *sql.DB }

// Open opens the global state database and initializes account progress/statistics.
func Open(path string) (*DB, error) {
	db, err := storage.OpenSQLite(path, false)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS manga_progress (
		user_id INTEGER NOT NULL,
		manga_id TEXT NOT NULL,
		chapter_index INTEGER NOT NULL,
		page_index INTEGER NOT NULL,
		scroll_offset_ratio REAL NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(user_id,manga_id)
	);
	CREATE TABLE IF NOT EXISTS text_progress (
		user_id INTEGER NOT NULL, document_id TEXT NOT NULL,
		start_line INTEGER NOT NULL, byte_offset INTEGER NOT NULL,
		chapter_index INTEGER NOT NULL, file_version TEXT NOT NULL,
		updated_at TEXT NOT NULL, PRIMARY KEY(user_id,document_id)
	);
	CREATE TABLE IF NOT EXISTS user_game_stats (
		user_id INTEGER NOT NULL, game_id TEXT NOT NULL,
		play_time_ms INTEGER NOT NULL DEFAULT 0,
		last_played_at TEXT NOT NULL, PRIMARY KEY(user_id,game_id)
	)`)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	rows, err := db.Query("PRAGMA table_info(manga_progress)")
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	hasMode := false
	hasVersion := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			return nil, errors.Join(err, rows.Close(), db.Close())
		}
		if name == "position_mode" {
			hasMode = true
		}
		if name == "file_version" {
			hasVersion = true
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if !hasMode {
		if _, err := db.Exec("ALTER TABLE manga_progress ADD COLUMN position_mode TEXT NOT NULL DEFAULT 'book'"); err != nil {
			return nil, errors.Join(err, db.Close())
		}
	}
	if !hasVersion {
		if _, err := db.Exec("ALTER TABLE manga_progress ADD COLUMN file_version TEXT NOT NULL DEFAULT ''"); err != nil {
			return nil, errors.Join(err, db.Close())
		}
	}
	return &DB{db: db}, nil
}

// Close checkpoints and releases the global state connection pool.
func (d *DB) Close() error { return d.db.Close() }

// SaveManga persists a UUID-qualified book position for one authenticated account.
// An identical position does not update SQLite pages or timestamps.
func (d *DB) SaveManga(ctx context.Context, p *manga.MangaProgress) error {
	if p == nil || p.UserID <= 0 || strings.IndexByte(p.MangaID, ':') < 1 || len(p.MangaID) > 4096 || p.ChapterIndex < 0 || p.ChapterIndex > 20000 || p.PageIndex < 0 || p.PageIndex > 20000 || math.IsNaN(p.ScrollOffsetRatio) || math.IsInf(p.ScrollOffsetRatio, 0) || p.ScrollOffsetRatio < 0 || p.ScrollOffsetRatio > 1 {
		return ErrInvalidProgress
	}
	mode := p.PositionMode
	if mode == "" {
		mode = "book"
	}
	if (mode != "page" && mode != "book") || len(p.FileVersion) > 128 {
		return ErrInvalidProgress
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO manga_progress(user_id,manga_id,chapter_index,page_index,scroll_offset_ratio,updated_at,position_mode,file_version)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,manga_id) DO UPDATE SET
		chapter_index=excluded.chapter_index,page_index=excluded.page_index,
		scroll_offset_ratio=excluded.scroll_offset_ratio,updated_at=excluded.updated_at,position_mode=excluded.position_mode,file_version=excluded.file_version
		WHERE manga_progress.chapter_index!=excluded.chapter_index OR manga_progress.page_index!=excluded.page_index OR manga_progress.scroll_offset_ratio!=excluded.scroll_offset_ratio OR manga_progress.position_mode!=excluded.position_mode OR manga_progress.file_version!=excluded.file_version`,
		p.UserID, p.MangaID, p.ChapterIndex, p.PageIndex, p.ScrollOffsetRatio, time.Now().UTC().Format(time.RFC3339), mode, p.FileVersion)
	return err
}

// Manga reads a single user's book position; an unseen book returns nil.
func (d *DB) Manga(ctx context.Context, userID int64, id string) (*manga.MangaProgress, error) {
	if userID <= 0 || id == "" || len(id) > 4096 {
		return nil, ErrInvalidProgress
	}
	var p manga.MangaProgress
	var updated string
	err := d.db.QueryRowContext(ctx, `SELECT user_id,manga_id,chapter_index,page_index,scroll_offset_ratio,updated_at,position_mode,file_version FROM manga_progress WHERE user_id=? AND manga_id=?`, userID, id).
		Scan(&p.UserID, &p.MangaID, &p.ChapterIndex, &p.PageIndex, &p.ScrollOffsetRatio, &updated, &p.PositionMode, &p.FileVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if p.ChapterIndex < 0 || p.ChapterIndex > 20000 || p.PageIndex < 0 || p.PageIndex > 20000 || math.IsNaN(p.ScrollOffsetRatio) || math.IsInf(p.ScrollOffsetRatio, 0) || p.ScrollOffsetRatio < 0 || p.ScrollOffsetRatio > 1 || (p.PositionMode != "book" && p.PositionMode != "page") {
		return nil, ErrInvalidProgress
	}
	p.UpdatedAt, err = time.Parse(time.RFC3339, updated)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
