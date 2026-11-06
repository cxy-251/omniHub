package shortvideo

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PartialsPlan 汇总短视频库中待清理的残片文件（.part/.ytdl/.temp.mp4/.aria2/.tmp/.rotate.mp4 及 0 字节空媒体）。
type PartialsPlan struct {
	Files    int      `json:"files"`
	Bytes    int64    `json:"bytes"`
	Creators []string `json:"creators"`
	paths    []string
}

func isPartialFile(name string, size int64) bool {
	lower := strings.ToLower(name)
	if lower == "downloaded.txt" || strings.HasPrefix(lower, "index.db") || strings.HasPrefix(lower, "thumbnails.db") || strings.HasPrefix(lower, "albums.db") {
		return false
	}
	for _, suffix := range []string{".part", ".ytdl", ".temp.mp4", ".aria2", ".tmp", ".rotate.mp4"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	if size == 0 && (strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".mp3")) {
		return true
	}
	return false
}

// PlanPartials 扫描多个 shortvideo 目录中的残片文件，返回数量、总字节数与预览样本。
func PlanPartials(dirs []string) (PartialsPlan, error) {
	var plan PartialsPlan
	seen := make(map[string]bool)
	for _, dir := range dirs {
		cleanDir := filepath.Clean(dir)
		if seen[cleanDir] {
			continue
		}
		seen[cleanDir] = true
		root, err := os.OpenRoot(cleanDir)
		if err != nil {
			continue
		}
		_ = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() && (d.Name() == ".thumbs" || strings.HasPrefix(d.Name(), ".Trash")) {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if !isPartialFile(d.Name(), info.Size()) {
				return nil
			}
			full := filepath.Join(cleanDir, filepath.FromSlash(rel))
			plan.Files++
			plan.Bytes += info.Size()
			plan.paths = append(plan.paths, full)
			if len(plan.Creators) < 20 {
				plan.Creators = append(plan.Creators, rel)
			}
			return nil
		})
		_ = root.Close()
	}
	return plan, nil
}

// CleanPartials 将 PlanPartials 识别出的残片文件移入系统回收站。
func CleanPartials(ctx context.Context, plan PartialsPlan, trash func(string) error, onProgress func(done int)) (int, error) {
	cleaned := 0
	var errs []error
	for i, p := range plan.paths {
		if err := ctx.Err(); err != nil {
			return cleaned, err
		}
		if err := trash(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			if len(errs) < 3 {
				errs = append(errs, err)
			}
		} else {
			cleaned++
		}
		if onProgress != nil {
			onProgress(i + 1)
		}
	}
	return cleaned, errors.Join(errs...)
}
