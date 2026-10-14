package games

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var gameFolderPrefix = regexp.MustCompile(`^(\d+)[\s\-_]+(.*)$`)

// ValidCategory checks a bounded portable category identifier, including 3ds.
func ValidCategory(category string) bool {
	if category == "" || len(category) > 32 {
		return false
	}
	for _, c := range category {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// FolderIdentity derives the stable prefix ID and default label; an unnumbered
// folder retains its full name as a legacy ID. Display labels never select paths.
func FolderIdentity(name string) (string, string) {
	match := gameFolderPrefix.FindStringSubmatch(name)
	if len(match) == 3 {
		return match[1], match[2]
	}
	return name, name
}

// ResolveIndexedDirectory verifies the indexed directory or searches at most
// 20,000 immediate category entries for an old prefix ID. It never guesses from
// a display title, follows category/game links, or updates the database.
func ResolveIndexedDirectory(ctx context.Context, gamesDir string, game *Game) (result string, resultErr error) {
	if game == nil || game.ID == "" || len(game.ID) > 512 || !ValidCategory(game.Category) {
		return "", errors.New("invalid game directory identity")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(gamesDir)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	category := game.Category + "_games"
	info, err := root.Lstat(category)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("game category must be a real directory")
	}
	if game.RelDir != "" {
		if len(game.RelDir) > 4096 || strings.Contains(game.RelDir, "\\") || path.Clean(game.RelDir) != game.RelDir || strings.Count(game.RelDir, "/") != 1 || !strings.HasPrefix(game.RelDir, category+"/") {
			return "", errors.New("indexed directory escapes category")
		}
		id, _ := FolderIdentity(path.Base(game.RelDir))
		if id != game.ID {
			return "", errors.New("indexed directory prefix differs from game ID")
		}
		info, err := root.Lstat(filepath.FromSlash(game.RelDir))
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("game directory must be real")
		}
		return game.RelDir, ctx.Err()
	}
	directory, err := root.Open(category)
	if err != nil {
		return "", err
	}
	entries, readErr := directory.ReadDir(20001)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, directory.Close()); err != nil {
		return "", err
	}
	if len(entries) > 20000 {
		return "", errors.New("legacy game category exceeds capacity")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		id, _ := FolderIdentity(entry.Name())
		if id != game.ID {
			continue
		}
		if result != "" {
			return "", errors.New("ambiguous legacy game directory")
		}
		result = category + "/" + entry.Name()
	}
	if result == "" {
		return "", os.ErrNotExist
	}
	return result, nil
}
