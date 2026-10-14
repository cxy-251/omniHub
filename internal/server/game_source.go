package server

import (
	"context"
	"errors"
	"omnihub/internal/games"
	"os"
	"path/filepath"
	"strings"
)

type gameSource interface {
	GetGame(context.Context, string) (*games.Game, error)
	ListCatalogPage(context.Context, string, string, string, int) ([]*games.Game, error)
	Close() error
}

func (r *Router) activeGameCategories() []string {
	result := make([]string, 0)
	for _, directory := range r.libMgr.ActiveDirectories() {
		if strings.HasPrefix(directory, "standalone_games/") && strings.HasSuffix(directory, "_games") {
			result = append(result, strings.TrimSuffix(strings.TrimPrefix(directory, "standalone_games/"), "_games"))
		}
	}
	return result
}

// Indexes take precedence; only a missing index uses read-only discovery.
// Corrupt or inaccessible indexes remain errors, never a silent fallback.
func (r *Router) openGameSource(ctx context.Context, directory string) (gameSource, error) {
	if _, err := os.Stat(filepath.Join(directory, "games.db")); errors.Is(err, os.ErrNotExist) {
		return r.gameDiscovery.Open(ctx, directory, r.activeGameCategories())
	} else if err != nil {
		return nil, err
	}
	return games.OpenDBReadOnly(directory)
}
