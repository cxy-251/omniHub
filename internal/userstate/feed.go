package userstate

import (
	"context"
	"errors"
	"math"
	"time"
)

// 推荐与点赞的通用存储（短视频、相册等共用，domain 区分内容类型）：
//   item_likes    账号点赞的条目
//   group_pins    账号固定的分组（短视频博主等），按固定先后
//   feed_profile  账号的偏好：标签 → 权重，按半衰期衰减
//   feed_seen     账号看过的条目（次数、最近时间）
//   item_hidden   移到「回收站」的条目（不写原库的内容用这个隐藏，如只读相册；全局，可恢复）

const feedSchema = `
CREATE TABLE IF NOT EXISTS item_likes (
	user_id INTEGER NOT NULL, domain TEXT NOT NULL, item_id TEXT NOT NULL, created_at TEXT NOT NULL,
	PRIMARY KEY(user_id, domain, item_id)
);
CREATE TABLE IF NOT EXISTS group_pins (
	user_id INTEGER NOT NULL, domain TEXT NOT NULL, group_id TEXT NOT NULL, created_at TEXT NOT NULL,
	PRIMARY KEY(user_id, domain, group_id)
);
CREATE TABLE IF NOT EXISTS feed_profile (
	user_id INTEGER NOT NULL, domain TEXT NOT NULL, tag TEXT NOT NULL, weight REAL NOT NULL, updated_at INTEGER NOT NULL,
	PRIMARY KEY(user_id, domain, tag)
);
CREATE TABLE IF NOT EXISTS feed_seen (
	user_id INTEGER NOT NULL, domain TEXT NOT NULL, item_id TEXT NOT NULL, count INTEGER NOT NULL, last_at INTEGER NOT NULL,
	PRIMARY KEY(user_id, domain, item_id)
);
CREATE TABLE IF NOT EXISTS item_hidden (
	domain TEXT NOT NULL, item_id TEXT NOT NULL, library_id TEXT NOT NULL, created_at TEXT NOT NULL,
	PRIMARY KEY(domain, item_id)
);
-- 旧的短视频点赞与固定博主并入通用表（重复执行无副作用）。
INSERT OR IGNORE INTO item_likes SELECT user_id, 'shortvideo', post_id, created_at FROM shortvideo_likes;
INSERT OR IGNORE INTO group_pins SELECT user_id, 'shortvideo', creator_id, created_at FROM shortvideo_pins;
`

// FeedHalfLife 是偏好的半衰期：两周前的行为影响减半。
const FeedHalfLife = 14 * 24 * time.Hour

// ErrInvalidFeed 表示账号、类型或条目标识不合规。
var ErrInvalidFeed = errors.New("invalid feed request")

func validFeed(userID int64, domain, id string) bool {
	return userID > 0 && domain != "" && len(domain) <= 32 && id != "" && len(id) <= 2048
}

func decay(weight float64, from, to int64) float64 {
	if to <= from {
		return weight
	}
	return weight * math.Pow(0.5, float64(to-from)/FeedHalfLife.Seconds())
}

// SetLike 点赞或取消。
func (d *DB) SetLike(ctx context.Context, userID int64, domain, itemID string, liked bool) error {
	if !validFeed(userID, domain, itemID) {
		return ErrInvalidFeed
	}
	if !liked {
		_, err := d.db.ExecContext(ctx, "DELETE FROM item_likes WHERE user_id=? AND domain=? AND item_id=?", userID, domain, itemID)
		return err
	}
	_, err := d.db.ExecContext(ctx, "INSERT OR IGNORE INTO item_likes VALUES(?,?,?,?)", userID, domain, itemID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// Likes 返回点赞的条目，最近点赞的在前。
func (d *DB) Likes(ctx context.Context, userID int64, domain string) ([]string, error) {
	return d.strings(ctx, "SELECT item_id FROM item_likes WHERE user_id=? AND domain=? ORDER BY created_at DESC", userID, domain)
}

// SetPin 固定或取消固定一个分组。
func (d *DB) SetPin(ctx context.Context, userID int64, domain, groupID string, pinned bool) error {
	if !validFeed(userID, domain, groupID) {
		return ErrInvalidFeed
	}
	if !pinned {
		_, err := d.db.ExecContext(ctx, "DELETE FROM group_pins WHERE user_id=? AND domain=? AND group_id=?", userID, domain, groupID)
		return err
	}
	_, err := d.db.ExecContext(ctx, "INSERT OR IGNORE INTO group_pins VALUES(?,?,?,?)", userID, domain, groupID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// Pins 返回固定的分组，按固定先后。
func (d *DB) Pins(ctx context.Context, userID int64, domain string) ([]string, error) {
	return d.strings(ctx, "SELECT group_id FROM group_pins WHERE user_id=? AND domain=? ORDER BY created_at, rowid", userID, domain)
}

// AdjustProfile 把一次行为计入偏好：每个标签的权重先按时间衰减，再加上 delta。
func (d *DB) AdjustProfile(ctx context.Context, userID int64, domain string, tags []string, delta float64) error {
	if userID <= 0 || len(tags) == 0 || delta == 0 || math.IsNaN(delta) {
		return nil
	}
	now := time.Now().Unix()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, tag := range tags {
		var w float64
		var at int64
		err := tx.QueryRowContext(ctx, "SELECT weight, updated_at FROM feed_profile WHERE user_id=? AND domain=? AND tag=?", userID, domain, tag).Scan(&w, &at)
		if err != nil {
			w, at = 0, now
		}
		w = math.Max(-20, math.Min(20, decay(w, at, now)+delta))
		if _, err := tx.ExecContext(ctx, `INSERT INTO feed_profile VALUES(?,?,?,?,?)
			ON CONFLICT(user_id, domain, tag) DO UPDATE SET weight=excluded.weight, updated_at=excluded.updated_at`, userID, domain, tag, w, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Profile 返回衰减到现在的偏好权重（绝对值很小的忽略）。
func (d *DB) Profile(ctx context.Context, userID int64, domain string) (map[string]float64, error) {
	out := map[string]float64{}
	if userID <= 0 {
		return out, nil
	}
	rows, err := d.db.QueryContext(ctx, "SELECT tag, weight, updated_at FROM feed_profile WHERE user_id=? AND domain=?", userID, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().Unix()
	for rows.Next() {
		var tag string
		var w float64
		var at int64
		if err := rows.Scan(&tag, &w, &at); err != nil {
			return nil, err
		}
		if w = decay(w, at, now); math.Abs(w) >= 0.01 {
			out[tag] = w
		}
	}
	return out, rows.Err()
}

// MarkSeen 记一次看过。
func (d *DB) MarkSeen(ctx context.Context, userID int64, domain, itemID string) error {
	if !validFeed(userID, domain, itemID) {
		return ErrInvalidFeed
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO feed_seen VALUES(?,?,?,1,?)
		ON CONFLICT(user_id, domain, item_id) DO UPDATE SET count=count+1, last_at=excluded.last_at`, userID, domain, itemID, time.Now().Unix())
	return err
}

// SeenItems 返回看过的条目（次数、最近时间）。
func (d *DB) SeenItems(ctx context.Context, userID int64, domain string) (map[string][2]int64, error) {
	out := map[string][2]int64{}
	if userID <= 0 {
		return out, nil
	}
	rows, err := d.db.QueryContext(ctx, "SELECT item_id, count, last_at FROM feed_seen WHERE user_id=? AND domain=?", userID, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var c, at int64
		if err := rows.Scan(&id, &c, &at); err != nil {
			return nil, err
		}
		out[id] = [2]int64{c, at}
	}
	return out, rows.Err()
}

// SetHidden 把条目移入或移出 omniHub 的回收站（隐藏，不改原库）。
func (d *DB) SetHidden(ctx context.Context, domain, itemID, libraryID string, hidden bool) error {
	if domain == "" || itemID == "" || len(itemID) > 2048 {
		return ErrInvalidFeed
	}
	if !hidden {
		_, err := d.db.ExecContext(ctx, "DELETE FROM item_hidden WHERE domain=? AND item_id=?", domain, itemID)
		return err
	}
	_, err := d.db.ExecContext(ctx, "INSERT OR REPLACE INTO item_hidden VALUES(?,?,?,?)", domain, itemID, libraryID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// Hidden 返回回收站里的条目（条目 ID → 资源库 ID），最近移入的在前由调用方排序。
func (d *DB) Hidden(ctx context.Context, domain string) (map[string]string, error) {
	out := map[string]string{}
	rows, err := d.db.QueryContext(ctx, "SELECT item_id, library_id FROM item_hidden WHERE domain=?", domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, lib string
		if err := rows.Scan(&id, &lib); err != nil {
			return nil, err
		}
		out[id] = lib
	}
	return out, rows.Err()
}

func (d *DB) strings(ctx context.Context, query string, args ...any) ([]string, error) {
	out := []string{}
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RemoveLikes 删除所有账号对某条目的点赞（条目被移走后调用）。
func (d *DB) RemoveLikes(ctx context.Context, domain, itemID string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM item_likes WHERE domain=? AND item_id=?", domain, itemID)
	return err
}
