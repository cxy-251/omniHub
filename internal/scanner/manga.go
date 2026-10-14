package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/catalog"
	"omnihub/internal/manga"
)

func mangaIndexDirectory(root string) (string, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	for _, name := range []string{"manga.db", "manga.db-wal", "manga.db-shm"} {
		info, err := os.Lstat(filepath.Join(canonical, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("manga index or sidecar must be a regular file")
		}
	}
	return canonical, nil
}

// IndexMangaFile validates one published book against its category boundary and
// inserts metadata without renumbering existing paths or resetting user covers.
func IndexMangaFile(ctx context.Context, root, file string) (result error) {
	root, err := mangaIndexDirectory(root)
	if err != nil {
		return err
	}
	file, err = filepath.EvalSymlinks(file)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, file)
	if err != nil || !filepath.IsLocal(relative) || strings.ContainsAny(relative, "\\\x00:") {
		return errors.New("archive is outside manga category")
	}
	book, err := manga.ScanMangaFromCBZContext(ctx, file)
	if err != nil {
		return err
	}
	book.RelPath = filepath.ToSlash(relative)
	db, err := manga.OpenDB(root)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	return db.UpsertIndexed(ctx, book)
}

// ScanMangaContext traverses a bounded recursive catalog, then reads one archive
// at a time. Cancellation/errors are reported; originals and old rows survive.
func ScanMangaContext(ctx context.Context, root, scope string) (count int, result error) {
	root, err := mangaIndexDirectory(root)
	if err != nil {
		return 0, err
	}
	scope, err = filepath.EvalSymlinks(scope)
	if err != nil {
		return 0, err
	}
	relative, err := filepath.Rel(root, scope)
	if err != nil || !filepath.IsLocal(relative) {
		return 0, errors.New("scan scope outside manga category")
	}
	directory := catalog.New(".cbz")
	db, err := manga.OpenDB(root)
	if err != nil {
		return 0, err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	paths, err := directory.SnapshotPaths(ctx, scope)
	if err != nil {
		return count, err
	}
	for _, path := range paths {
		if err = ctx.Err(); err != nil {
			return count, err
		}
		file := filepath.Join(scope, filepath.FromSlash(path))
		book, err := manga.ScanMangaFromCBZContext(ctx, file)
		if err != nil {
			return count, fmt.Errorf("read %q: %w", path, err)
		}
		rel, err := filepath.Rel(root, file)
		if err != nil || !filepath.IsLocal(rel) {
			return count, errors.New("archive boundary changed")
		}
		book.RelPath = filepath.ToSlash(rel)
		if err = db.UpsertIndexed(ctx, book); err != nil {
			return count, fmt.Errorf("index %q: %w", path, err)
		}
		count++
	}
	return count, nil
}
