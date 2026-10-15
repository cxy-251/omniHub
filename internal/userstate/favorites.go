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

// SetShortVideoLike 给短视频作品点赞或取消（作品 ID 为「平台/博主/文件名」，按账号独立）。
func (d *DB) SetShortVideoLike(ctx context.Context, userID int64, postID string, liked bool) error {
	if userID <= 0 || postID == "" || len(postID) > 2048 {
		return ErrInvalidGamePlay
	}
	if !liked {
		_, err := d.db.ExecContext(ctx, "DELETE FROM shortvideo_likes WHERE user_id=? AND post_id=?", userID, postID)
		return err
	}
	_, err := d.db.ExecContext(ctx, "INSERT OR IGNORE INTO shortvideo_likes(user_id, post_id, created_at) VALUES(?,?,?)",
		userID, postID, time.Now().UTC().Format(time.RFC3339))
	return err
}

// ShortVideoLikes 返回账号点赞的作品 ID 集合。
func (d *DB) ShortVideoLikes(ctx context.Context, userID int64) (map[string]bool, error) {
	out := map[string]bool{}
	if userID <= 0 {
		return out, nil
	}
	rows, err := d.db.QueryContext(ctx, "SELECT post_id FROM shortvideo_likes WHERE user_id=?", userID)
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

// RemoveShortVideoLikes 删除所有账号对某作品的点赞（作品移到回收站后调用）。
func (d *DB) RemoveShortVideoLikes(ctx context.Context, postID string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM shortvideo_likes WHERE post_id=?", postID)
	return err
}

// SetShortVideoPin 固定或取消固定一个博主（博主 ID 为「平台/博主目录」，按账号独立）。
func (d *DB) SetShortVideoPin(ctx context.Context, userID int64, creatorID string, pinned bool) error {
	if userID <= 0 || creatorID == "" || len(creatorID) > 1024 {
		return ErrInvalidGamePlay
	}
	if !pinned {
		_, err := d.db.ExecContext(ctx, "DELETE FROM shortvideo_pins WHERE user_id=? AND creator_id=?", userID, creatorID)
		return err
	}
	_, err := d.db.ExecContext(ctx, "INSERT OR IGNORE INTO shortvideo_pins(user_id, creator_id, created_at) VALUES(?,?,?)",
		userID, creatorID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ShortVideoPins 返回账号固定的博主，按固定的先后排序。
func (d *DB) ShortVideoPins(ctx context.Context, userID int64) ([]string, error) {
	out := []string{}
	if userID <= 0 {
		return out, nil
	}
	rows, err := d.db.QueryContext(ctx, "SELECT creator_id FROM shortvideo_pins WHERE user_id=? ORDER BY created_at, rowid", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
