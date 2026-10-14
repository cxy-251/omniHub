package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"omnihub/internal/games"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func gameEntries(root *os.Root, name string) ([]os.DirEntry, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	entries, readErr := file.ReadDir(20001)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	if len(entries) > 20000 {
		return nil, errors.New("game directory exceeds entry capacity")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// ScanGames indexes direct numbered game folders with bounded metadata reads.
// Paths come from enumeration, not display names. Invalid/duplicate metadata
// returns an error before any records are changed; publication is one transaction.
func ScanGames(standaloneGamesDir string) (int, error) {
	return ScanGamesContext(context.Background(), standaloneGamesDir)
}

// ScanGamesContext cancels enumeration/publication and publishes one complete snapshot.
func ScanGamesContext(ctx context.Context, standaloneGamesDir string) (int, error) {
	return ScanGameCategories(ctx, standaloneGamesDir, nil)
}

// ScanGameCategories scans only explicit active categories; nil scans all.
// Disabled categories are neither read nor marked missing.
func ScanGameCategories(ctx context.Context, standaloneGamesDir string, categories []string) (count int, resultErr error) {
	if len(categories) > 128 {
		return 0, errors.New("scan categories exceed capacity")
	}
	allowed := make(map[string]bool, len(categories))
	for _, category := range categories {
		if !games.ValidCategory(category) {
			return 0, errors.New("invalid scan category")
		}
		allowed[category] = true
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(standaloneGamesDir)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	directories, err := gameEntries(root, ".")
	if err != nil {
		return 0, err
	}
	indexed := make([]*games.Game, 0)
	retainedBytes := 0
	ids := make(map[string]bool)
	for _, category := range directories {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if !category.IsDir() || category.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(category.Name(), "_games") {
			continue
		}
		name := strings.TrimSuffix(category.Name(), "_games")
		if categories != nil && !allowed[name] {
			continue
		}
		target := "gui_only"
		if name == "rpg" || name == "slg" || name == "retro" || name == "flash" {
			target = "web_only"
		}
		entries, err := gameEntries(root, category.Name())
		if err != nil {
			return 0, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			id, title := games.FolderIdentity(entry.Name())
			if ids[id] {
				return 0, fmt.Errorf("duplicate game prefix %q", id)
			}
			ids[id] = true
			game := &games.Game{ID: id, Name: title, Category: name, Target: target, RelDir: category.Name() + "/" + entry.Name()}
			configPath := filepath.Join(category.Name(), entry.Name(), "omni.json")
			info, err := root.Lstat(configPath)
			if err == nil {
				if !info.Mode().IsRegular() || info.Size() > 64<<10 {
					return 0, errors.New("game metadata must be a regular file within 64KiB")
				}
				file, err := root.Open(configPath)
				if err != nil {
					return 0, err
				}
				data, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
				if err := errors.Join(readErr, file.Close()); err != nil {
					return 0, err
				}
				if len(data) > 64<<10 {
					return 0, errors.New("game metadata exceeds capacity")
				}
				var meta struct {
					Name        string          `json:"name"`
					Exe         string          `json:"exe"`
					Args        json.RawMessage `json:"args"`
					Env         json.RawMessage `json:"env"`
					Icon        string          `json:"icon"`
					ProtonAppID string          `json:"proton_appid"`
				}
				if err := json.Unmarshal(data, &meta); err != nil {
					return 0, fmt.Errorf("invalid game metadata: %w", err)
				}
				if meta.Name != "" {
					game.Name = meta.Name
				}
				game.Exe = meta.Exe
				game.Args, err = launchMetadataField(meta.Args, '[')
				if err != nil {
					return 0, err
				}
				game.Env, err = launchMetadataField(meta.Env, '{')
				if err != nil {
					return 0, err
				}
				game.Icon = meta.Icon
				game.ProtonAppID = meta.ProtonAppID
			} else if !errors.Is(err, os.ErrNotExist) {
				return 0, err
			}
			if err := games.ValidateIndexGame(game); err != nil {
				return 0, fmt.Errorf("invalid game %q: %w", game.RelDir, err)
			}
			if _, _, err := games.ParseLaunchFields(game.Args, game.Env); err != nil {
				return 0, fmt.Errorf("invalid launch metadata for %q: %w", game.RelDir, err)
			}
			indexed = append(indexed, game)
			retainedBytes += 256 + len(game.ID) + len(game.Name) + len(game.RelDir) + len(game.Exe) + len(game.Args) + len(game.Env) + len(game.Icon) + len(game.ProtonAppID)
			if retainedBytes > 8<<20 {
				return 0, errors.New("game scan metadata exceeds memory budget")
			}
			if len(indexed) > 20000 {
				return 0, errors.New("game index exceeds capacity")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	db, err := games.OpenDB(standaloneGamesDir)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	publish := db.PublishScan
	if categories != nil {
		publish = func(ctx context.Context, list []*games.Game) error {
			return db.PublishCategories(ctx, list, categories)
		}
	}
	if err := publish(ctx, indexed); err != nil {
		return 0, err
	}
	return len(indexed), nil
}

func launchMetadataField(raw json.RawMessage, expected byte) (string, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return "", nil
	}
	if strings.HasPrefix(text, `"`) {
		var legacy string
		err := json.Unmarshal(raw, &legacy)
		return legacy, err
	}
	if text[0] != expected {
		return "", errors.New("invalid launch metadata field type")
	}
	return text, nil
}
