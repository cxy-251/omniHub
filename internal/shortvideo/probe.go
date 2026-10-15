package shortvideo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os/exec"
	"strconv"

	"omnihub/internal/platform"
)

// PendingProbes 返回最多 limit 个尚未探测编码与尺寸的视频，新作品优先。
func (s *Store) PendingProbes(limit int) ([]Post, error) {
	rows, err := s.index.Query("SELECT "+postColumns+" FROM posts WHERE kind='video' AND codec='' ORDER BY published DESC, id DESC LIMIT ?", limit)
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

// ProbeVideo 用低优先级 ffprobe 读取编码、显示宽高（按旋转调整）与时长并写入 index.db。
// 文件无法解析时记 codec='?'，文件变化前不再重试。
func (s *Store) ProbeVideo(ctx context.Context, dir string, p Post) error {
	if s.readOnly {
		return errors.New("read-only library")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return ErrToolMissing
	}
	codec, width, height, duration := "?", 0, 0, 0.0
	if path, err := ResolveFile(dir, p.ID); err == nil {
		if info, err := ffprobe(ctx, path); err == nil {
			codec, width, height, duration = info.codec, info.width, info.height, info.duration
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// 只在文件未变时写入，避免覆盖扫描期间更新的记录。
	_, err := s.index.Exec("UPDATE posts SET codec=?, width=?, height=?, duration=? WHERE id=? AND size=? AND mtime=?",
		codec, width, height, duration, p.ID, p.Size, p.MTime)
	return err
}

type probeInfo struct {
	codec         string
	width, height int
	duration      float64
}

func ffprobe(ctx context.Context, path string) (probeInfo, error) {
	cmd := platform.BackgroundCommand(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height:stream_side_data=rotation:stream_tags=rotate:format=duration",
		"-of", "json", path)
	var out bytes.Buffer
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return probeInfo{}, err
	}
	if err := cmd.Start(); err != nil {
		return probeInfo{}, err
	}
	_, copyErr := io.Copy(&out, io.LimitReader(stdout, 64<<10))
	if err := errors.Join(copyErr, cmd.Wait()); err != nil {
		return probeInfo{}, err
	}
	var parsed struct {
		Streams []struct {
			CodecName string            `json:"codec_name"`
			Width     int               `json:"width"`
			Height    int               `json:"height"`
			Tags      map[string]string `json:"tags"`
			SideData  []struct {
				Rotation float64 `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil || len(parsed.Streams) == 0 {
		return probeInfo{}, errors.New("no video stream")
	}
	st := parsed.Streams[0]
	info := probeInfo{codec: st.CodecName, width: st.Width, height: st.Height}
	info.duration, _ = strconv.ParseFloat(parsed.Format.Duration, 64)
	rotation := 0.0
	if len(st.SideData) > 0 {
		rotation = st.SideData[0].Rotation
	} else if r, err := strconv.ParseFloat(st.Tags["rotate"], 64); err == nil {
		rotation = r
	}
	// 旋转 90/270 度时显示宽高互换。
	if int(math.Abs(rotation))%180 == 90 {
		info.width, info.height = info.height, info.width
	}
	if info.codec == "" {
		info.codec = "?"
	}
	return info, nil
}
