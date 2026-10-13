package scanner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"omnihub/internal/games"
	"omnihub/internal/manga"
	"omnihub/internal/shortvideo"
)

var idRegexp = regexp.MustCompile(`^(\d+)[\s\-_]+(.*)$`)

// ScanGames 扫描资源库下的 standalone_games 目录并写入 games.db。
func ScanGames(standaloneGamesDir string) (int, error) {
	db, err := games.OpenDB(standaloneGamesDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	entries, err := os.ReadDir(standaloneGamesDir)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, catEntry := range entries {
		if !catEntry.IsDir() || !strings.HasSuffix(catEntry.Name(), "_games") {
			continue
		}

		catName := strings.TrimSuffix(catEntry.Name(), "_games")
		target := "gui_only"
		if catName == "rpg" || catName == "slg" || catName == "retro" || catName == "flash" {
			target = "web_only"
		}

		catPath := filepath.Join(standaloneGamesDir, catEntry.Name())
		gameDirs, err := os.ReadDir(catPath)
		if err != nil {
			continue
		}

		for _, gd := range gameDirs {
			if !gd.IsDir() {
				continue
			}

			dirName := gd.Name()
			gameID := dirName
			gameName := dirName

			matches := idRegexp.FindStringSubmatch(dirName)
			if len(matches) == 3 {
				gameID = matches[1]
				gameName = matches[2]
			}

			g := &games.Game{
				ID:       gameID,
				Name:     gameName,
				Category: catName,
				Target:   target,
				Exe:      "",
			}

			// 读取可选的 omni.json
			omniPath := filepath.Join(catPath, dirName, "omni.json")
			if data, err := os.ReadFile(omniPath); err == nil {
				var meta struct {
					Name        string `json:"name"`
					Exe         string `json:"exe"`
					Args        string `json:"args"`
					Icon        string `json:"icon"`
					ProtonAppID string `json:"proton_appid"`
				}
				if json.Unmarshal(data, &meta) == nil {
					if meta.Name != "" {
						g.Name = meta.Name
					}
					g.Exe = meta.Exe
					g.Args = meta.Args
					g.Icon = meta.Icon
					g.ProtonAppID = meta.ProtonAppID
				}
			}

			if err := db.UpsertGame(g); err == nil {
				count++
			}
		}
	}

	return count, nil
}

// ScanManga 扫描 media_library/manga 目录下的 CBZ 文件并建立索引。
func ScanManga(mangaDir string) (int, error) {
	db, err := manga.OpenDB(mangaDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	entries, err := os.ReadDir(mangaDir)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".cbz") {
			continue
		}

		cbzPath := filepath.Join(mangaDir, entry.Name())
		m, err := manga.ScanMangaFromCBZ(cbzPath)
		if err != nil {
			continue
		}

		if err := db.UpsertManga(m); err == nil {
			count++
		}
	}

	return count, nil
}

// ScanShortVideos 扫描 media_library/shortvideo 目录下的博主与作品（视频/图集），提取缩略图并写入自治数据库。
func ScanShortVideos(shortvideoDir string, readOnly bool) (int, error) {
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

					// 缩略图检查与提取
					thumb, _ := storage.GetThumbnail(postID)
					if len(thumb) == 0 {
						if tb := extractVideoThumbnail(itemPath); len(tb) > 0 {
							_ = storage.SaveThumbnail(postID, tb)
						}
					}

					p := &shortvideo.Post{
						ID:        postID,
						CreatorID: creatorID,
						Title:     strings.TrimSuffix(itemName, filepath.Ext(itemName)),
						Type:      "video",
						RelPath:   relPath,
						CreatedAt: time.Now(),
					}
					if err := storage.UpsertPost(p); err == nil {
						itemCount++
						totalPosts++
					}
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

					sort.Strings(imgFiles)
					postID := fmt.Sprintf("%s_%s", creatorID, itemName)
					relPath, _ := filepath.Rel(shortvideoDir, itemPath)

					for idx, imgFile := range imgFiles {
						imgPath := filepath.Join(itemPath, imgFile)
						imgData, err := os.ReadFile(imgPath)
						if err != nil {
							continue
						}
						_ = storage.SaveAlbumImage(postID, idx+1, imgData)
						if idx == 0 {
							_ = storage.SaveThumbnail(postID, imgData)
						}
					}

					p := &shortvideo.Post{
						ID:        postID,
						CreatorID: creatorID,
						Title:     itemName,
						Type:      "album",
						RelPath:   relPath,
						CreatedAt: time.Now(),
					}
					if err := storage.UpsertPost(p); err == nil {
						itemCount++
						totalPosts++
						if !readOnly {
							_ = exec.Command("gio", "trash", itemPath).Run()
						}
					}
				}
			}

			creator.ItemCount = itemCount
			_ = storage.UpsertCreator(creator)
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

