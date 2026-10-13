package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"omnihub/internal/games"
	"omnihub/internal/manga"
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
