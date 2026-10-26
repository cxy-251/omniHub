package shortvideo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	_ "golang.org/x/image/webp"

	"omnihub/internal/platform"
	"omnihub/internal/storage"
)

// ErrNoThumbnail 表示作品还没有可用缩略图。
var ErrNoThumbnail = errors.New("thumbnail not available")

// ErrToolMissing 表示系统缺少 ffmpeg，缩略图生成无法进行。
var ErrToolMissing = errors.New("ffmpeg not found")

// Thumbnail 返回作品的封面 JPEG。生成失败的作品也返回 ErrNoThumbnail。
func (s *Store) Thumbnail(postID string) ([]byte, error) {
	var data []byte
	err := s.thumbs.QueryRow("SELECT data FROM thumbnails WHERE post_id=? AND failed=0 AND length(data) <= ?", postID, storage.MaxThumbnailReadBytes).Scan(&data)
	if err != nil || len(data) == 0 {
		return nil, ErrNoThumbnail
	}
	return data, nil
}

// PendingThumbnails 按发布日期倒序返回最多 limit 个缺少或过期缩略图的作品。
// 生成失败且源文件未变化的作品不再返回，避免反复对坏文件调用 ffmpeg。
func (s *Store) PendingThumbnails(limit int) ([]Post, error) {
	type signature struct{ size, mtime int64 }
	done := make(map[string]signature)
	rows, err := s.thumbs.Query("SELECT post_id, source_size, source_mtime FROM thumbnails")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var sig signature
		if err := rows.Scan(&id, &sig.size, &sig.mtime); err != nil {
			rows.Close()
			return nil, err
		}
		done[id] = sig
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	rows, err = s.index.Query("SELECT " + postColumns + " FROM posts ORDER BY published DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []Post
	for rows.Next() && len(pending) < limit {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		if sig, ok := done[p.ID]; !ok || sig.size != p.Size || sig.mtime != p.MTime {
			pending = append(pending, p)
		}
	}
	return pending, rows.Err()
}

// MakeThumbnail 用低优先级 ffmpeg 生成作品封面并写入 thumbnails.db。
// 视频取第 1 秒（过短则取首帧），图集取第一张图。源文件损坏时记录失败标记并返回 nil。
func (s *Store) MakeThumbnail(ctx context.Context, dir string, p Post) error {
	if s.readOnly {
		return errors.New("read-only library")
	}
	if data, w, h := legacyThumbnail(dir, p); data != nil {
		return s.saveThumbnail(p, w, h, data)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ErrToolMissing
	}
	var data []byte
	var err error
	switch {
	case p.Kind == KindVideo:
		var path string
		if path, err = ResolveFile(dir, p.ID); err == nil {
			data, err = ffmpegFrame(ctx, path, nil, "1")
			if err == nil && len(data) == 0 {
				data, err = ffmpegFrame(ctx, path, nil, "")
			}
		}
	case p.OnDisk:
		var names []string
		if names, err = s.diskAlbumImages(dir, p.ID); err == nil {
			var path string
			if path, err = ResolveFile(dir, p.ID+"/"+names[0]); err == nil {
				data, err = ffmpegFrame(ctx, path, nil, "")
			}
		}
	default:
		var img []byte
		if img, err = s.AlbumImage(p.ID, 1); err == nil {
			data, err = ffmpegFrame(ctx, "pipe:0", img, "")
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	width, height := 0, 0
	if err == nil && len(data) > 0 {
		if cfg, _, decodeErr := image.DecodeConfig(bytes.NewReader(data)); decodeErr == nil {
			width, height = cfg.Width, cfg.Height
		}
	}
	if err != nil || width == 0 {
		width, height, data = 0, 0, nil
	}
	return s.saveThumbnail(p, width, height, data)
}

// saveThumbnail 写入封面；data 为空表示生成失败（源文件不变时不再重试）。
func (s *Store) saveThumbnail(p Post, width, height int, data []byte) error {
	failed := len(data) == 0
	if failed {
		data = []byte{}
	}
	_, err := s.thumbs.Exec(`INSERT INTO thumbnails (post_id, source_size, source_mtime, width, height, failed, data) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(post_id) DO UPDATE SET source_size=excluded.source_size, source_mtime=excluded.source_mtime,
		width=excluded.width, height=excluded.height, failed=excluded.failed, data=excluded.data`,
		p.ID, p.Size, p.MTime, width, height, failed, data)
	return err
}

// legacyThumbnail 读取 z6x hub 以前生成的封面（作品旁边的 .thumbs/<文件名>.webp，360 像素宽），
// 转成 JPEG 返回，省掉在手机上重新跑 ffmpeg。封面比作品旧、读不了或解码失败时返回 nil。
func legacyThumbnail(dir string, p Post) (data []byte, width, height int) {
	rel := filepath.FromSlash(p.ID) // 视频是文件，磁盘上的图集是文件夹，封面名都是「名字.webp」
	if !filepath.IsLocal(rel) {
		return nil, 0, 0
	}
	path := filepath.Join(dir, rel)
	legacy := filepath.Join(filepath.Dir(path), ".thumbs", filepath.Base(path)+".webp")
	st, err := os.Stat(legacy)
	if err != nil || st.ModTime().UnixNano() < p.MTime || st.Size() > 4<<20 {
		return nil, 0, 0
	}
	f, err := os.Open(legacy)
	if err != nil {
		return nil, 0, 0
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, 0, 0
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82}); err != nil {
		return nil, 0, 0
	}
	b := img.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy()
}

// ffmpegFrame 输出 480px 宽 JPEG。input 为 "pipe:0" 时从 stdin 读取图片字节。
func ffmpegFrame(ctx context.Context, input string, stdin []byte, seek string) ([]byte, error) {
	args := []string{"-nostdin", "-v", "error"}
	if stdin != nil {
		args = args[1:]
	}
	if seek != "" {
		args = append(args, "-ss", seek)
	}
	args = append(args, "-i", input, "-frames:v", "1",
		"-vf", "scale="+strconv.Itoa(ThumbnailWidth)+":-2", "-c:v", "mjpeg", "-q:v", "6", "-f", "image2pipe", "-")
	cmd := platform.BackgroundCommand(ctx, "ffmpeg", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	_, copyErr := io.Copy(&out, io.LimitReader(stdout, storage.MaxThumbnailReadBytes+1))
	waitErr := cmd.Wait()
	if err := errors.Join(copyErr, waitErr); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	if out.Len() > storage.MaxThumbnailReadBytes {
		return nil, errors.New("thumbnail exceeds size limit")
	}
	return out.Bytes(), nil
}

// ResolveFile 在 dir 内解析相对斜杠路径，拒绝越界、符号链接与非普通文件。
func ResolveFile(dir, rel string) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat(filepath.FromSlash(rel))
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	return filepath.Join(dir, filepath.FromSlash(rel)), nil
}
