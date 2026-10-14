package server

import (
	"errors"
	"omnihub/internal/library"
	"os"
	"path/filepath"
)

// gameStorageDirectory checks the shared index/log area, which is a module
// parent rather than an independently configured playable category. Game
// launches still resolve and authorize their concrete category separately.
func (r *Router) gameStorageDirectory(root string) (directory string, resultErr error) {
	for _, lib := range r.libMgr.ListLibraries() {
		if !library.SameRoot(lib.RootPath, root) {
			continue
		}
		if !lib.IsOnline {
			return "", errors.New("game library offline")
		}
		canonical, err := filepath.EvalSymlinks(lib.RootPath)
		if err != nil {
			return "", err
		}
		boundary, err := os.OpenRoot(canonical)
		if err != nil {
			return "", err
		}
		defer func() { resultErr = errors.Join(resultErr, boundary.Close()) }()
		info, err := boundary.Lstat("standalone_games")
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("game module must be a real directory")
		}
		for _, name := range []string{"games.db", "games.db-wal", "games.db-shm", "logs"} {
			info, err := boundary.Lstat(filepath.Join("standalone_games", name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("game storage file or log directory cannot be a link")
			}
		}
		return filepath.Join(canonical, "standalone_games"), nil
	}
	return "", errors.New("game library not mounted")
}
