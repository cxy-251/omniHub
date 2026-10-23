package gallery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// 与 gallery 项目（cmd/gallery 的 pack-vol / build-hot）同一套规格：照片 ID 是内容 SHA-256，
// 缩略图固定宽 480、JPEG 质量 80，色系由缩略图 16×16 抽样得出，created_at 用 "2006-01-02 15:04:05"。
const (
	thumbnailWidth   = 480
	thumbnailQuality = 80
	maxImportBytes   = 256 << 20
	createdAtLayout  = "2006-01-02 15:04:05"
)

// ImportProgress 是导入进度；Skipped 含重复与不支持的文件。
type ImportProgress struct {
	Total, Done, Added, Duplicates, Unsupported int
	Errors                                      []string // 最多保留 20 条
}

// ImportDirectory 把 srcDir（含子目录）里的图片导入 galleryDir：原图写入 originals/ 当前未满 4GB 的分卷，
// 照片记录与缩略图在同一个事务里写入 gallery.db。先写原图再写记录，中途中断不会出现找不到原图的照片。
// 已有的照片（同内容）跳过；源文件不改动。onProgress 每处理一个文件调用一次。
func ImportDirectory(ctx context.Context, galleryDir, srcDir string, onProgress func(ImportProgress)) (ImportProgress, error) {
	var files []string
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && path != srcDir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	progress := ImportProgress{Total: len(files)}
	if err != nil {
		return progress, err
	}
	meta, err := OpenMetaDB(galleryDir)
	if err != nil {
		return progress, err
	}
	defer meta.Close()
	volumes, err := NewVolumeManager(filepath.Join(galleryDir, "originals"))
	if err != nil {
		return progress, err
	}
	defer volumes.Close()

	fail := func(path string, err error) {
		if len(progress.Errors) < 20 {
			progress.Errors = append(progress.Errors, filepath.Base(path)+"："+err.Error())
		}
	}
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return progress, err
		}
		added, err := importOne(meta, volumes, path)
		switch {
		case errors.Is(err, errDuplicate):
			progress.Duplicates++
		case errors.Is(err, errUnsupported):
			progress.Unsupported++
		case err != nil:
			fail(path, err)
		case added:
			progress.Added++
		}
		progress.Done++
		if onProgress != nil {
			onProgress(progress)
		}
	}
	return progress, nil
}

var (
	errDuplicate   = errors.New("duplicate")
	errUnsupported = errors.New("unsupported")
)

func importOne(meta *MetaDB, volumes *VolumeManager, path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	info, err := f.Stat()
	if err != nil || info.Size() == 0 || info.Size() > maxImportBytes {
		f.Close()
		return false, errUnsupported
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return false, err
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return false, errUnsupported
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	var exists int
	if err := meta.db.QueryRow("SELECT COUNT(*) FROM photos WHERE id = ?", id).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		return false, errDuplicate
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false, errUnsupported
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return false, errUnsupported
	}
	thumbHeight := max(1, int(math.Round(float64(bounds.Dy())*thumbnailWidth/float64(bounds.Dx()))))
	thumb := image.NewRGBA(image.Rect(0, 0, thumbnailWidth, thumbHeight))
	xdraw.BiLinear.Scale(thumb, thumb.Bounds(), img, bounds, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return false, err
	}
	family, colorHex := dominantColor(thumb)

	volume, err := volumes.WriteOriginal(id, data)
	if err != nil {
		return false, err
	}
	tx, err := meta.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO photos (id, filename, size_bytes, mime_type, width, height, volume, color_family, color_hex, is_favorite, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		id, filepath.Base(path), len(data), mime, bounds.Dx(), bounds.Dy(), volume, family, colorHex, info.ModTime().Format(createdAtLayout)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO thumbnails (photo_id, size_type, width, height, data) VALUES (?, 'm', ?, ?, ?)`,
		id, thumbnailWidth, thumbHeight, buf.Bytes()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// dominantColor 与 gallery 项目的 ExtractDominantColor 相同：16×16 格网抽样，按 10 个色系计数，
// 彩色色系权重 1.3，取最多的色系及其平均色。
func dominantColor(img image.Image) (int, string) {
	b := img.Bounds()
	stepX, stepY := max(1, b.Dx()/16), max(1, b.Dy()/16)
	var counts [10]int
	var sumR, sumG, sumB [10]int64
	for y := b.Min.Y + stepY/2; y < b.Max.Y; y += stepY {
		for x := b.Min.X + stepX/2; x < b.Max.X; x += stepX {
			r32, g32, b32, _ := img.At(x, y).RGBA()
			r, g, bl := uint8(r32>>8), uint8(g32>>8), uint8(b32>>8)
			cat := classifyColor(rgbToHSL(r, g, bl))
			counts[cat]++
			sumR[cat] += int64(r)
			sumG[cat] += int64(g)
			sumB[cat] += int64(bl)
		}
	}
	best, bestScore := 0, 0.0
	for i, n := range counts {
		score := float64(n)
		if i != 7 && i != 8 && i != 9 {
			score *= 1.3
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if counts[best] == 0 {
		return -1, "#888888"
	}
	n := int64(counts[best])
	return best, fmt.Sprintf("#%02x%02x%02x", sumR[best]/n, sumG[best]/n, sumB[best]/n)
}

func rgbToHSL(r, g, b uint8) (h, s, l float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	hi, lo := math.Max(rf, math.Max(gf, bf)), math.Min(rf, math.Min(gf, bf))
	delta := hi - lo
	l = (hi + lo) / 2
	if delta == 0 {
		return 0, 0, l
	}
	if l < 0.5 {
		s = delta / (hi + lo)
	} else {
		s = delta / (2 - hi - lo)
	}
	switch hi {
	case rf:
		h = (gf - bf) / delta
		if gf < bf {
			h += 6
		}
	case gf:
		h = (bf-rf)/delta + 2
	default:
		h = (rf-gf)/delta + 4
	}
	return h * 60, s, l
}

// classifyColor：0 红 1 橙 2 黄 3 绿 4 蓝 5 紫 6 粉 7 白/浅色 8 黑/深色 9 棕灰。
func classifyColor(h, s, l float64) int {
	switch {
	case l < 0.18:
		return 8
	case l > 0.85 && s < 0.25:
		return 7
	case s < 0.18:
		return 9
	case h >= 345 || h < 15:
		return 0
	case h < 45:
		if l < 0.4 {
			return 9
		}
		return 1
	case h < 75:
		return 2
	case h < 165:
		return 3
	case h < 260:
		return 4
	case h < 315:
		return 5
	default:
		return 6
	}
}

// parseStamp 读 created_at / deleted_at：gallery 项目写的是 "2006-01-02 15:04:05"，旧版 omniHub 写 RFC3339。
func parseStamp(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	t, _ := time.ParseInLocation(createdAtLayout, s, time.Local)
	return t
}
