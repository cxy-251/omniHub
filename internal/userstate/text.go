package userstate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// TextPosition identifies the current text page or EPUB chapter, tied to a file revision.
// ByteOffset is a physical byte cursor; FileVersion is assigned by the service.
type TextPosition struct {
	UserID       int64     `json:"user_id"`
	DocumentID   string    `json:"document_id"`
	StartLine    int       `json:"start_line"`
	ByteOffset   int64     `json:"byte_offset"`
	ChapterIndex int       `json:"chapter_index"`
	FileVersion  string    `json:"file_version"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func validTextPosition(p *TextPosition) bool {
	return p != nil && p.UserID > 0 && strings.IndexByte(p.DocumentID, ':') >= 1 && len(p.DocumentID) <= 4096 && p.StartLine >= 1 && p.StartLine <= 10000000 && p.ByteOffset >= 0 && p.ByteOffset <= 1<<50 && p.ChapterIndex >= 0 && p.ChapterIndex <= 20000 && p.FileVersion != "" && len(p.FileVersion) <= 128
}

// SaveText stores a single authenticated account's bounded, revision-specific position.
// Identical positions do not change the row or its timestamp.
func (d *DB) SaveText(ctx context.Context, p *TextPosition) error {
	if !validTextPosition(p) {
		return ErrInvalidProgress
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO text_progress(user_id,document_id,start_line,byte_offset,chapter_index,file_version,updated_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(user_id,document_id) DO UPDATE SET start_line=excluded.start_line,
		byte_offset=excluded.byte_offset,chapter_index=excluded.chapter_index,file_version=excluded.file_version,updated_at=excluded.updated_at
		WHERE text_progress.start_line!=excluded.start_line OR text_progress.byte_offset!=excluded.byte_offset OR text_progress.chapter_index!=excluded.chapter_index OR text_progress.file_version!=excluded.file_version`,
		p.UserID, p.DocumentID, p.StartLine, p.ByteOffset, p.ChapterIndex, p.FileVersion, time.Now().UTC().Format(time.RFC3339))
	return err
}

// Text reads a single user's position; an unseen document returns nil.
func (d *DB) Text(ctx context.Context, userID int64, id string) (*TextPosition, error) {
	if userID <= 0 || id == "" || len(id) > 4096 {
		return nil, ErrInvalidProgress
	}
	var p TextPosition
	var updated string
	err := d.db.QueryRowContext(ctx, `SELECT user_id,document_id,start_line,byte_offset,chapter_index,file_version,updated_at FROM text_progress WHERE user_id=? AND document_id=?`, userID, id).
		Scan(&p.UserID, &p.DocumentID, &p.StartLine, &p.ByteOffset, &p.ChapterIndex, &p.FileVersion, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validTextPosition(&p) {
		return nil, ErrInvalidProgress
	}
	p.UpdatedAt, err = time.Parse(time.RFC3339, updated)
	return &p, err
}
