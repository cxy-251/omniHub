package userstate

import "context"

// RenameItems 把一批条目换成新 ID（资源在库里搬了位置时用）：阅读进度、点赞、看过、回收站一起改。
// 新 ID 已经有记录时保留新的、丢掉旧的。pairs 为「旧 ID → 新 ID」。
func (d *DB) RenameItems(ctx context.Context, domain string, pairs map[string]string) error {
	if len(pairs) == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	type stmt struct{ update, drop string }
	stmts := []stmt{
		{"UPDATE OR IGNORE item_likes SET item_id=? WHERE domain=? AND item_id=?", "DELETE FROM item_likes WHERE domain=? AND item_id=?"},
		{"UPDATE OR IGNORE feed_seen SET item_id=? WHERE domain=? AND item_id=?", "DELETE FROM feed_seen WHERE domain=? AND item_id=?"},
		{"UPDATE OR IGNORE item_hidden SET item_id=? WHERE domain=? AND item_id=?", "DELETE FROM item_hidden WHERE domain=? AND item_id=?"},
	}
	exec := func(q string, args ...any) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}
	for from, to := range pairs {
		for _, s := range stmts {
			if err := exec(s.update, to, domain, from); err != nil {
				return err
			}
			if err := exec(s.drop, domain, from); err != nil {
				return err
			}
		}
		if domain == "manga" {
			if err := exec("UPDATE OR IGNORE manga_progress SET manga_id=? WHERE manga_id=?", to, from); err != nil {
				return err
			}
			if err := exec("DELETE FROM manga_progress WHERE manga_id=?", from); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
