package server

import (
	"errors"
	"net/http"
	"omnihub/internal/auth"
	"omnihub/internal/games"
	"os"
	"sort"
	"strconv"
)

type gameItem struct {
	*games.Game
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	ReadOnly    bool   `json:"read_only"`
}

func (r *Router) listGameLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	category := req.URL.Query().Get("category")
	if category != "" && !games.ValidCategory(category) {
		http.Error(w, "invalid game category", 400)
		return
	}
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	// Prefer the owner's primary copy, then stable UUID order. Removing the
	// primary still yields every other mounted library without path guessing.
	sort.SliceStable(libraries, func(i, j int) bool {
		if libraries[i].IsPrimary != libraries[j].IsPrimary {
			return libraries[i].IsPrimary
		}
		return libraries[i].ID < libraries[j].ID
	})
	target := "web_only"
	if auth.ClientFromContext(req.Context()) == auth.TargetGUI {
		target = "gui_only"
	}
	seen := make(map[string]bool)
	retained, scanned := 0, 0
	items := make([]gameItem, 0, limit+1)
	for _, lib := range libraries {
		if len(items) > limit {
			break
		}
		directory, err := r.gameStorageDirectory(lib.RootPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			http.Error(w, "game storage boundary invalid", 503)
			return
		}
		db, err := r.openGameSource(req.Context(), directory)
		if err != nil {
			http.Error(w, "game index unavailable", 503)
			return
		}
		readErr := func() error {
			after := ""
			for len(items) <= limit {
				batch, err := db.ListCatalogPage(req.Context(), target, category, after, 32)
				if err != nil {
					return err
				}
				for _, game := range batch {
					if err := req.Context().Err(); err != nil {
						return err
					}
					scanned++
					if scanned > 100000 {
						return errors.New("game aggregation exceeds scan budget")
					}
					after = game.ID
					if r.libMgr.IsCategoryDisabled("standalone_games/"+game.Category+"_games") || seen[game.ID] {
						continue
					}
					retained += len(game.ID) + 64
					if retained > 8<<20 {
						return errors.New("game identities exceed aggregation budget")
					}
					seen[game.ID] = true
					if offset > 0 {
						offset--
						continue
					}
					items = append(items, gameItem{Game: game, LibraryID: lib.ID, LibraryName: lib.Name, ReadOnly: lib.ReadOnly})
					if len(items) > limit {
						break
					}
				}
				if len(batch) < 32 {
					break
				}
			}
			return nil
		}()
		if err := errors.Join(readErr, db.Close()); err != nil {
			http.Error(w, "game catalog unavailable: "+err.Error(), 503)
			return
		}
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(more))
	writeJSON(w, 200, items)
}
