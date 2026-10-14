package scanner

import (
	"context"
	"fmt"
	"omnihub/internal/ordering"
	"omnihub/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"omnihub/internal/shortvideo"
)

// ScanManga 扫描 media_library/manga 目录下的 CBZ 文件并建立索引。
func ScanManga(mangaDir string) (int, error) {
	return ScanMangaContext(context.Background(), mangaDir, mangaDir)
}

// ScanShortVideos 扫描 media_library/shortvideo 目录下的博主与作品（视频/图集），提取缩略图并写入自治数据库。
func ScanShortVideos(shortvideoDir string, readOnly bool) (int, error) {
	if readOnly {
		return 0, fmt.Errorf("cannot import short videos into a read-only library")
	}
	return scanShortVideos(shortvideoDir, platform.Trash)
}

func scanShortVideos(shortvideoDir string, trash func(string) error) (int, error) {
	storage, err := shortvideo.OpenStorage(shortvideoDir)
	if err != nil {
		return 0, err
	}
	defer storage.Close()

	platformEntries, err := os.ReadDir(shortvideoDir)
	if err != nil {
		return 0, err
	}

	totalPosts := 0

	// 遍历平台层或直接博主层
	for _, pe := range platformEntries {
		if !pe.IsDir() || strings.HasPrefix(pe.Name(), ".") {
			continue
		}

		platformName := pe.Name()
		platformPath := filepath.Join(shortvideoDir, platformName)

		creatorEntries, err := os.ReadDir(platformPath)
		if err != nil {
			continue
		}

		for _, ce := range creatorEntries {
			if !ce.IsDir() || strings.HasPrefix(ce.Name(), ".") {
				continue
			}

			creatorName := ce.Name()
			creatorID := fmt.Sprintf("%s_%s", platformName, creatorName)
			creatorPath := filepath.Join(platformPath, creatorName)

			creator := &shortvideo.Creator{
				ID:       creatorID,
				Name:     creatorName,
				Platform: platformName,
			}

			itemCount := 0
			items, err := os.ReadDir(creatorPath)
			if err != nil {
				continue
			}

			for _, item := range items {
				itemName := item.Name()
				if strings.HasPrefix(itemName, ".") {
					continue
				}
				itemPath := filepath.Join(creatorPath, itemName)

				if !item.IsDir() && strings.HasSuffix(strings.ToLower(itemName), ".mp4") {
					// 视频作品
					postID := fmt.Sprintf("%s_%s", creatorID, strings.TrimSuffix(itemName, filepath.Ext(itemName)))
					relPath, _ := filepath.Rel(shortvideoDir, itemPath)

					p := &shortvideo.Post{
						ID:        postID,
						CreatorID: creatorID,
						Title:     strings.TrimSuffix(itemName, filepath.Ext(itemName)),
						Type:      "video",
						RelPath:   relPath,
						CreatedAt: time.Now(),
					}
					if err := storage.UpsertPost(p); err != nil {
						return totalPosts, fmt.Errorf("index video: %w", err)
					}
					itemCount++
					totalPosts++
				} else if item.IsDir() {
					// 散装图集目录
					albumImages, err := os.ReadDir(itemPath)
					if err != nil {
						continue
					}

					var imgFiles []string
					for _, f := range albumImages {
						if f.IsDir() {
							continue
						}
						ext := strings.ToLower(filepath.Ext(f.Name()))
						if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
							imgFiles = append(imgFiles, f.Name())
						}
					}

					if len(imgFiles) == 0 {
						continue
					}

					postID := fmt.Sprintf("%s_%s", creatorID, itemName)
					relPath, _ := filepath.Rel(shortvideoDir, itemPath)

					p := &shortvideo.Post{
						ID:        postID,
						CreatorID: creatorID,
						Title:     itemName,
						Type:      "album",
						RelPath:   relPath,
						CreatedAt: time.Now(),
					}
					sort.Slice(imgFiles, func(i, j int) bool { return ordering.NaturalLess(imgFiles[i], imgFiles[j]) })
					files := make([]string, len(imgFiles))
					for i, name := range imgFiles {
						files[i] = filepath.Join(itemPath, name)
					}
					if err := storage.ImportAlbumFiles(postID, files); err != nil {
						return totalPosts, fmt.Errorf("import album %s: %w", itemName, err)
					}
					if err := storage.UpsertPost(p); err != nil {
						return totalPosts, fmt.Errorf("index album: %w", err)
					}
					itemCount++
					totalPosts++
					if trash != nil {
						if err := storage.VerifyAlbumSources(postID, files); err != nil {
							return totalPosts, fmt.Errorf("preserve source album %s: %w", itemName, err)
						}
						if err := trash(itemPath); err != nil {
							return totalPosts, fmt.Errorf("trash imported album: %w", err)
						}
					}
				}
			}

			creator.ItemCount = itemCount
			if err := storage.UpsertCreator(creator); err != nil {
				return totalPosts, fmt.Errorf("index creator: %w", err)
			}
		}
	}

	return totalPosts, nil
}

func extractVideoThumbnail(videoPath string) []byte {
	cmd := exec.Command("ffmpeg", "-ss", "00:00:01", "-i", videoPath, "-vframes", "1", "-f", "image2", "-vf", "scale=480:-1", "-")
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		return out
	}
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	for _, ext := range []string{".jpg", ".jpeg", ".png"} {
		if b, err := os.ReadFile(base + ext); err == nil && len(b) > 0 {
			return b
		}
	}
	return nil
}
