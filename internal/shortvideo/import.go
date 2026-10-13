package shortvideo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxAlbumImageBytes = 32 * 1024 * 1024

// ImportAlbumFiles 按已排序文件清单事务写入图集，并逐张回读校验 SHA-256。
// 单次仅持有一张原图；任何错误均要求调用方保留源目录。
func (s *Storage) ImportAlbumFiles(postID string, files []string) error {
	if postID == "" || len(files) == 0 || len(files) > 10000 {
		return errors.New("invalid album file list")
	}
	tx, err := s.albumDB.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM album_images WHERE post_id = ?", postID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM album_image_hashes WHERE post_id = ?", postID); err != nil {
		return err
	}
	for index, path := range files {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("album image is not a regular file: %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, maxAlbumImageBytes+1))
		closeErr := f.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		if len(data) == 0 || len(data) > maxAlbumImageBytes {
			return fmt.Errorf("invalid image size: %s", path)
		}
		hash := sha256.Sum256(data)
		if _, err := tx.Exec("INSERT INTO album_images(post_id, order_index, data) VALUES (?, ?, ?)", postID, index+1, data); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO album_image_hashes(post_id, order_index, sha256, size_bytes) VALUES (?, ?, ?, ?)", postID, index+1, hash[:], len(data)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit album images: %w", err)
	}
	for index := range files {
		var data, expected []byte
		var size int
		if err := s.albumDB.QueryRow(`SELECT i.data, h.sha256, h.size_bytes FROM album_images i
		JOIN album_image_hashes h ON i.post_id=h.post_id AND i.order_index=h.order_index
		WHERE i.post_id=? AND i.order_index=?`, postID, index+1).Scan(&data, &expected, &size); err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		if len(data) != size || string(hash[:]) != string(expected) {
			return errors.New("album integrity verification failed")
		}
	}
	return nil
}

// VerifyAlbumSources 再次核对源目录，新增、修改或非图片文件均阻止清理。
// 调用方应对目录施加写任务互斥锁；此检查不能阻止外部进程随后修改文件。
func (s *Storage) VerifyAlbumSources(postID string, files []string) error {
	if len(files) == 0 {
		return errors.New("empty source album")
	}
	entries, err := os.ReadDir(filepath.Dir(files[0]))
	if err != nil {
		return err
	}
	if len(entries) != len(files) {
		return errors.New("source album contains unimported files")
	}
	for index, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		size, readErr := io.Copy(hash, io.LimitReader(f, maxAlbumImageBytes+1))
		closeErr := f.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		var expected []byte
		var expectedSize int64
		if err := s.albumDB.QueryRow("SELECT sha256, size_bytes FROM album_image_hashes WHERE post_id=? AND order_index=?", postID, index+1).Scan(&expected, &expectedSize); err != nil {
			return err
		}
		if size != expectedSize || string(hash.Sum(nil)) != string(expected) {
			return errors.New("source image changed during import")
		}
	}
	return nil
}
