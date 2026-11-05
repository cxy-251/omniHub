package userstate

import (
	"context"
	"errors"
	"time"
)

const musicSchema = `
CREATE TABLE IF NOT EXISTS music_ratings (
 user_id INTEGER NOT NULL, track_id TEXT NOT NULL, rating INTEGER NOT NULL CHECK(rating IN (-1,1)), updated_at TEXT NOT NULL,
 PRIMARY KEY(user_id,track_id)
);
CREATE TABLE IF NOT EXISTS music_artist_groups (
 user_id INTEGER NOT NULL, artist_id TEXT NOT NULL, main INTEGER NOT NULL CHECK(main IN (0,1)),
 PRIMARY KEY(user_id,artist_id)
);
CREATE TABLE IF NOT EXISTS music_trash (
 id TEXT PRIMARY KEY, user_id INTEGER NOT NULL, track_id TEXT NOT NULL, title TEXT NOT NULL,
 artist TEXT NOT NULL, library_root TEXT NOT NULL, receipts TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending', error TEXT NOT NULL DEFAULT ''
);`

// SetMusicRating atomically sets like (1), dislike (-1), or neutral (0).
func (d *DB) SetMusicRating(ctx context.Context, user int64, id string, rating int) error {
	if user <= 0 || id == "" || len(id) > 2048 || rating < -1 || rating > 1 {
		return ErrInvalidFeed
	}
	if rating == 0 {
		_, err := d.db.ExecContext(ctx, "DELETE FROM music_ratings WHERE user_id=? AND track_id=?", user, id)
		return err
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO music_ratings VALUES(?,?,?,?) ON CONFLICT(user_id,track_id) DO UPDATE SET rating=excluded.rating,updated_at=excluded.updated_at`, user, id, rating, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// MusicRatings returns only the current account's explicit choices.
func (d *DB) MusicRatings(ctx context.Context, user int64) (map[string]int, error) {
	out := map[string]int{}
	rows, err := d.db.QueryContext(ctx, "SELECT track_id,rating FROM music_ratings WHERE user_id=?", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// SetMusicArtistGroup overrides automatic provenance-based grouping without moving files.
func (d *DB) SetMusicArtistGroup(ctx context.Context, user int64, id string, main bool) error {
	if !validFeed(user, "music", id) {
		return ErrInvalidFeed
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO music_artist_groups VALUES(?,?,?) ON CONFLICT(user_id,artist_id) DO UPDATE SET main=excluded.main`, user, id, main)
	return err
}

// MusicArtistGroups returns explicit grouping overrides; absence means automatic.
func (d *DB) MusicArtistGroups(ctx context.Context, user int64) (map[string]bool, error) {
	out := map[string]bool{}
	rows, err := d.db.QueryContext(ctx, "SELECT artist_id,main FROM music_artist_groups WHERE user_id=?", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var main bool
		if err := rows.Scan(&id, &main); err != nil {
			return nil, err
		}
		out[id] = main
	}
	return out, rows.Err()
}

// MusicTrash is a recoverable operation journal. Receipts are server-only OS trash identities.
type MusicTrash struct {
	ID          string `json:"id"`
	UserID      int64  `json:"-"`
	TrackID     string `json:"track_id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	LibraryRoot string `json:"-"`
	Receipts    string `json:"-"`
	CreatedAt   string `json:"created_at"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// CreateMusicTrash records intent before any media is moved.
func (d *DB) CreateMusicTrash(ctx context.Context, t MusicTrash) error {
	if t.UserID <= 0 || t.ID == "" || t.TrackID == "" {
		return ErrInvalidFeed
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO music_trash(id,user_id,track_id,title,artist,library_root,created_at) VALUES(?,?,?,?,?,?,?)`, t.ID, t.UserID, t.TrackID, t.Title, t.Artist, t.LibraryRoot, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// UpdateMusicTrash preserves recovery details, including partially completed batches.
func (d *DB) UpdateMusicTrash(ctx context.Context, id, receipts, status, message string) error {
	if status != "trashed" && status != "partial" && status != "failed" && status != "restored" {
		return errors.New("invalid trash status")
	}
	_, err := d.db.ExecContext(ctx, "UPDATE music_trash SET receipts=?,status=?,error=? WHERE id=?", receipts, status, message, id)
	return err
}

// MusicTrashRecords returns this account's recoverable records, newest first.
func (d *DB) MusicTrashRecords(ctx context.Context, user int64) ([]MusicTrash, error) {
	out := []MusicTrash{}
	rows, err := d.db.QueryContext(ctx, `SELECT id,user_id,track_id,title,artist,library_root,receipts,created_at,status,error FROM music_trash WHERE user_id=? AND status IN ('pending','trashed','partial') ORDER BY created_at DESC LIMIT 2000`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t MusicTrash
		if err := rows.Scan(&t.ID, &t.UserID, &t.TrackID, &t.Title, &t.Artist, &t.LibraryRoot, &t.Receipts, &t.CreatedAt, &t.Status, &t.Error); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
