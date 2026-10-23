package shortvideo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"omnihub/internal/storage"
)

// MergeResult 是一次跨设备合并的结果。
type MergeResult struct {
	Albums    int      `json:"albums"`    // 新并入的图集
	Same      int      `json:"same"`      // 本库已有且内容一致（按每张图的 SHA-256）
	Conflicts []string `json:"conflicts"` // 同一作品 ID、内容不同：保留本库的，不覆盖
	Posts     int      `json:"posts"`     // 为并入的图集补上的作品记录
	Thumbs    int      `json:"thumbs"`    // 补上的封面缩略图
}

// MergeFrom 把另一台设备拷来的短视频数据库（srcDir 里的 albums.db，及可选的 index.db、thumbnails.db）
// 并入本库（ARCHITECTURE.md 2.3）。视频与未入库的图集目录是文件，用同步或拷贝工具放进对应博主目录后由扫描收录；
// 这里合并的是只存在于数据库里的图集（原目录已清理），以及它们的作品记录和封面。
// 外部库经 ATTACH 挂载（路径用绑定参数），在事务里合并；重复执行结果不变。
func (s *Store) MergeFrom(ctx context.Context, srcDir string) (MergeResult, error) {
	var result MergeResult
	if s.readOnly {
		return result, errors.New("只读库不能合并")
	}
	albums := filepath.Join(srcDir, "albums.db")
	if err := checkSource(albums); err != nil {
		return result, err
	}
	merged, err := s.mergeAlbums(ctx, albums, &result)
	if err != nil || len(merged) == 0 {
		return result, err
	}
	if index := filepath.Join(srcDir, "index.db"); checkSource(index) == nil {
		if result.Posts, err = s.mergePosts(ctx, index, merged); err != nil {
			return result, fmt.Errorf("图集已并入，作品记录合并失败：%w", err)
		}
	}
	if thumbs := filepath.Join(srcDir, "thumbnails.db"); checkSource(thumbs) == nil {
		if result.Thumbs, err = s.mergeThumbs(ctx, thumbs, merged); err != nil {
			return result, fmt.Errorf("图集已并入，封面合并失败：%w", err)
		}
	}
	return result, nil
}

// checkSource 只读打开外部数据库确认版本是本模块格式（2 以上、不高于当前版本）。
func checkSource(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("找不到 %s", path)
	}
	db, err := storage.OpenSQLite(path, true)
	if err != nil {
		return err
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("%s 不是 SQLite 数据库：%w", filepath.Base(path), err)
	}
	if version < 2 || version > SchemaVersion {
		return fmt.Errorf("%s 的版本是 %d，不是本程序的格式（2～%d）", filepath.Base(path), version, SchemaVersion)
	}
	return nil
}

// attach 在一条专用连接上挂载外部库为 src；返回的 conn 用完要 Close（先 DETACH）。
func attach(ctx context.Context, db *sql.DB, path string) (*sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS src", path); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func detach(conn *sql.Conn) {
	_, _ = conn.ExecContext(context.Background(), "DETACH DATABASE src")
	conn.Close()
}

func (s *Store) mergeAlbums(ctx context.Context, path string, result *MergeResult) ([]string, error) {
	conn, err := attach(ctx, s.albums, path)
	if err != nil {
		return nil, err
	}
	defer detach(conn)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT post_id FROM src.albums ORDER BY post_id")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	// 图集内容 = 按顺序的每张图 SHA-256。
	const digest = "SELECT COALESCE(group_concat(hex(sha256), ','), '') FROM (SELECT sha256 FROM %s.album_images WHERE post_id = ? ORDER BY order_index)"
	var merged []string
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM main.albums WHERE post_id = ?", id).Scan(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			var mine, theirs string
			if err := tx.QueryRowContext(ctx, fmt.Sprintf(digest, "main"), id).Scan(&mine); err != nil {
				return nil, err
			}
			if err := tx.QueryRowContext(ctx, fmt.Sprintf(digest, "src"), id).Scan(&theirs); err != nil {
				return nil, err
			}
			if mine == theirs {
				result.Same++
			} else {
				result.Conflicts = append(result.Conflicts, id)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO main.albums(post_id,image_count,total_bytes,imported_at) SELECT post_id,image_count,total_bytes,imported_at FROM src.albums WHERE post_id = ?", id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO main.album_images(post_id,order_index,filename,size_bytes,sha256,data) SELECT post_id,order_index,filename,size_bytes,sha256,data FROM src.album_images WHERE post_id = ?", id); err != nil {
			return nil, err
		}
		merged = append(merged, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	result.Albums = len(merged)
	return merged, nil
}

// mergePosts 为并入的图集补作品与博主记录：本库没有的按外部记录插入（原目录不在本机，on_disk=0、imported=1）；
// 本库已有（原目录在本机）的只标记为已入库。
func (s *Store) mergePosts(ctx context.Context, path string, ids []string) (int, error) {
	conn, err := attach(ctx, s.index, path)
	if err != nil {
		return 0, err
	}
	defer detach(conn)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	added := 0
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO main.creators(id,platform,name)
			SELECT c.id,c.platform,c.name FROM src.creators c JOIN src.posts p ON p.creator_id = c.id WHERE p.id = ?`, id); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO main.posts(id,creator_id,kind,title,published,size,mtime,image_count,music,on_disk,imported)
			SELECT id,creator_id,kind,title,published,size,mtime,image_count,music,0,1 FROM src.posts WHERE id = ? AND kind = 'album'`, id)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		} else if _, err := tx.ExecContext(ctx, "UPDATE main.posts SET imported = 1 WHERE id = ?", id); err != nil {
			return 0, err
		}
	}
	return added, tx.Commit()
}

// mergeThumbs 补上本库没有的封面（本库已有的保留）。
func (s *Store) mergeThumbs(ctx context.Context, path string, ids []string) (int, error) {
	conn, err := attach(ctx, s.thumbs, path)
	if err != nil {
		return 0, err
	}
	defer detach(conn)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	added := 0
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO main.thumbnails(post_id,source_size,source_mtime,width,height,failed,data)
			SELECT post_id,source_size,source_mtime,width,height,failed,data FROM src.thumbnails WHERE post_id = ? AND failed = 0`, id)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	return added, tx.Commit()
}
