package shortvideo

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"omnihub/internal/storage"
)

// ErrNotImported 表示图集尚未完整入库，不能清理原目录。
var ErrNotImported = errors.New("album not imported")

// diskAlbumImages 返回磁盘图集目录内按自然序排列的图片文件名。
func (s *Store) diskAlbumImages(dir, postID string) ([]string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	names, _, _, err := albumImages(root, postID)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("album has no images")
	}
	return names, nil
}

// DiskAlbumImage 返回磁盘图集第 index 张（从 1 开始）图片的物理路径。
func (s *Store) DiskAlbumImage(dir, postID string, index int) (string, error) {
	names, err := s.diskAlbumImages(dir, postID)
	if err != nil {
		return "", err
	}
	if index < 1 || index > len(names) {
		return "", os.ErrNotExist
	}
	return ResolveFile(dir, postID+"/"+names[index-1])
}

// AlbumImage 从 albums.db 读取第 index 张（从 1 开始）图片。
func (s *Store) AlbumImage(postID string, index int) ([]byte, error) {
	var data []byte
	err := s.albums.QueryRow("SELECT data FROM album_images WHERE post_id=? AND order_index=? AND length(data) <= ?",
		postID, index, storage.MaxImageReadBytes).Scan(&data)
	if err != nil {
		return nil, os.ErrNotExist
	}
	return data, nil
}

// ImportAlbum 把磁盘图集的图片写入 albums.db，提交后逐张回读校验 SHA-256，成功才标记 imported。
// 不修改、不移动任何源文件。
func (s *Store) ImportAlbum(dir string, p Post) error {
	if s.readOnly {
		return errors.New("read-only library")
	}
	if p.Kind != KindAlbum || !p.OnDisk {
		return errors.New("not an on-disk album")
	}
	names, err := s.diskAlbumImages(dir, p.ID)
	if err != nil {
		return err
	}
	tx, err := s.albums.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM album_images WHERE post_id=?", "DELETE FROM albums WHERE post_id=?"} {
		if _, err := tx.Exec(q, p.ID); err != nil {
			return err
		}
	}
	var total int64
	hashes := make([][]byte, len(names))
	for i, name := range names {
		data, err := readImage(dir, p.ID+"/"+name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		hashes[i] = sum[:]
		total += int64(len(data))
		if _, err := tx.Exec("INSERT INTO album_images (post_id, order_index, filename, size_bytes, sha256, data) VALUES (?,?,?,?,?,?)",
			p.ID, i+1, name, len(data), sum[:], data); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO albums (post_id, image_count, total_bytes, imported_at) VALUES (?,?,?,?)",
		p.ID, len(names), total, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for i := range names {
		data, err := s.AlbumImage(p.ID, i+1)
		if err != nil {
			return fmt.Errorf("read back image %d: %w", i+1, err)
		}
		if sum := sha256.Sum256(data); !bytes.Equal(sum[:], hashes[i]) {
			return fmt.Errorf("image %d failed verification", i+1)
		}
	}
	_, err = s.index.Exec("UPDATE posts SET imported=1 WHERE id=? AND size=? AND mtime=?", p.ID, p.Size, p.MTime)
	return err
}

// VerifyAndTrash 确认原目录与 albums.db 中的入库内容逐字节一致（文件名、数量、SHA-256，
// 且目录内没有其他文件），然后调用 trash 把目录移入回收站，并标记 on_disk=0。
// 任何不一致都返回错误并保留目录。
func (s *Store) VerifyAndTrash(dir string, p Post, trash func(string) error) error {
	if s.readOnly {
		return errors.New("read-only library")
	}
	if p.Kind != KindAlbum || !p.OnDisk || !p.Imported {
		return ErrNotImported
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	entries, err := readDir(root, p.ID)
	root.Close()
	if err != nil {
		return err
	}
	rows, err := s.albums.Query("SELECT filename, size_bytes, sha256 FROM album_images WHERE post_id=? ORDER BY order_index", p.ID)
	if err != nil {
		return err
	}
	type stored struct {
		size int64
		hash []byte
	}
	want := make(map[string]stored)
	for rows.Next() {
		var name string
		var st stored
		if err := rows.Scan(&name, &st.size, &st.hash); err != nil {
			rows.Close()
			return err
		}
		want[name] = st
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(want) == 0 || len(entries) != len(want) {
		return fmt.Errorf("%s: directory has %d entries, database has %d images", p.ID, len(entries), len(want))
	}
	for _, e := range entries {
		st, ok := want[e.Name()]
		if !ok || !e.Type().IsRegular() {
			return fmt.Errorf("%s: unexpected entry %q", p.ID, e.Name())
		}
		data, err := readImage(dir, p.ID+"/"+e.Name())
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(data); int64(len(data)) != st.size || !bytes.Equal(sum[:], st.hash) {
			return fmt.Errorf("%s: %q differs from imported copy", p.ID, e.Name())
		}
	}
	if err := trash(filepath.Join(dir, filepath.FromSlash(p.ID))); err != nil {
		return fmt.Errorf("move to trash: %w", err)
	}
	_, err = s.index.Exec("UPDATE posts SET on_disk=0 WHERE id=?", p.ID)
	return err
}

func readImage(dir, rel string) ([]byte, error) {
	path, err := ResolveFile(dir, rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, storage.MaxImageReadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > storage.MaxImageReadBytes {
		return nil, fmt.Errorf("%s: image size out of range", rel)
	}
	return data, nil
}

// AlbumsToImport 返回最多 limit 个原目录在磁盘、尚未入库的图集。
func (s *Store) AlbumsToImport(limit int) ([]Post, error) {
	return s.albumsWhere("on_disk=1 AND imported=0", limit)
}

// AlbumsToClean 返回最多 limit 个已入库且原目录仍在磁盘的图集。
func (s *Store) AlbumsToClean(limit int) ([]Post, error) {
	return s.albumsWhere("on_disk=1 AND imported=1", limit)
}

func (s *Store) albumsWhere(cond string, limit int) ([]Post, error) {
	rows, err := s.index.Query("SELECT "+postColumns+" FROM posts WHERE kind='album' AND "+cond+" ORDER BY id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}
