// Package storage centralizes bounded SQLite connection pools and safe URI construction.
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// OpenSQLite opens an existing database without writes when readOnly is true.
// Writable databases enable WAL; all connections enable foreign keys and bounded cache/pool sizes.
func OpenSQLite(path string, readOnly bool) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !readOnly {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
	}
	// SQLite opens its own sidecars after sql.Open. Reject links and special files
	// before connecting, including global state files which lack a media resolver.
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect database file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("database and sidecars must be regular files")
		}
	}
	uriPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{}
	q.Set("mode", "rwc")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "cache_size(-2048)")
	if readOnly {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("open database: %v; close: %w", err, closeErr)
		}
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

// ReadOnlyURI 返回参数绑定用的 SQLite 只读 URI，适用于外部 ATTACH 数据库。
func ReadOnlyURI(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	uriPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	u.RawQuery = "mode=ro"
	return u.String(), nil
}
