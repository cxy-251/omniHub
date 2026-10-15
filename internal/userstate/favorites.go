package userstate

import (
	"context"
	"time"
)

// SetGameFavorite 收藏或取消收藏一个游戏（游戏 ID 为「分类/目录名」，按账号独立）。
func (d *DB) SetGameFavorite(ctx context.Context, userID int64, gameID string, favorite bool) error {
	if userID <= 0 || !validGameID(gameID) {
		return ErrInvalidGamePlay
	}
	if !favorite {
		_, err := d.db.ExecContext(ctx, "DELETE FROM game_favorites WHERE user_id=? AND game_id=?", userID, gameID)
		return err
	}
	_, err := d.db.ExecContext(ctx, "INSERT OR IGNORE INTO game_favorites(user_id, game_id, created_at) VALUES(?,?,?)",
		userID, gameID, time.Now().UTC().Format(time.RFC3339))
	return err
}

// GameFavorites 返回账号收藏的游戏 ID 集合。
func (d *DB) GameFavorites(ctx context.Context, userID int64) (map[string]bool, error) {
	out := map[string]bool{}
	if userID <= 0 {
		return out, nil
	}
	rows, err := d.db.QueryContext(ctx, "SELECT game_id FROM game_favorites WHERE user_id=?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
