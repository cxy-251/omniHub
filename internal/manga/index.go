package manga

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// UpsertIndexed preserves an existing path's ID/custom cover/legacy history;
// new paths use a path digest, so equal basenames or metadata IDs cannot collide.
func (d *DB) UpsertIndexed(ctx context.Context, book *Manga) error {
	if book == nil || !filepath.IsLocal(filepath.FromSlash(book.RelPath)) || strings.ContainsAny(book.RelPath, "\\\x00:") {
		return errors.New("invalid manga index path")
	}
	rows, err := d.db.QueryContext(ctx, "SELECT id FROM mangas WHERE rel_path=? LIMIT 2", book.RelPath)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(ids) > 1 {
		return errors.New("duplicate manga index paths require repair")
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(book.RelPath)))
	if len(ids) == 1 {
		id = ids[0]
	}
	var stored string
	err = d.db.QueryRowContext(ctx, "SELECT rel_path FROM mangas WHERE id=?", id).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && stored != book.RelPath {
		return errors.New("manga index identity collision")
	}
	book.ID = id
	return d.UpsertMangaContext(ctx, book)
}
