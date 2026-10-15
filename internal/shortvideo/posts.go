package shortvideo

import (
	"database/sql"
	"strings"
)

// Kind 区分作品类型。
type Kind string

const (
	KindVideo Kind = "video"
	KindAlbum Kind = "album"
)

// Creator 是一个博主目录。
type Creator struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Name     string `json:"name"`
	Posts    int    `json:"posts"`
}

// Post 是一个作品：视频文件或图集目录。
type Post struct {
	ID         string `json:"id"`
	CreatorID  string `json:"creator_id"`
	Kind       Kind   `json:"kind"`
	Title      string `json:"title"`
	Published  string `json:"published"`
	Size       int64  `json:"size"`
	MTime      int64  `json:"-"`
	ImageCount int    `json:"image_count,omitempty"`
	Music      string `json:"music,omitempty"`
	OnDisk     bool   `json:"on_disk"`
	Imported   bool   `json:"imported"`
}

// Filter 选择作品列表；空字段表示不限。
type Filter struct {
	Platform  string
	CreatorID string
	Kind      Kind
}

const postColumns = "id, creator_id, kind, title, published, size, mtime, image_count, music, on_disk, imported"

func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.CreatorID, &p.Kind, &p.Title, &p.Published, &p.Size, &p.MTime, &p.ImageCount, &p.Music, &p.OnDisk, &p.Imported)
	return p, err
}

// Posts 按发布日期倒序返回一页作品。
func (s *Store) Posts(f Filter, limit, offset int) ([]Post, error) {
	where, args := f.clause()
	rows, err := s.index.Query("SELECT "+postColumns+" FROM posts"+where+" ORDER BY published DESC, id DESC LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// Post 返回单个作品；不存在时 ok 为 false。
func (s *Store) Post(id string) (p Post, ok bool, err error) {
	p, err = scanPost(s.index.QueryRow("SELECT "+postColumns+" FROM posts WHERE id=?", id))
	if err == sql.ErrNoRows {
		return p, false, nil
	}
	return p, err == nil, err
}

// Creators 返回博主及其作品数，platform 为空时返回全部。
func (s *Store) Creators(platform string) ([]Creator, error) {
	query := "SELECT c.id, c.platform, c.name, count(p.id) FROM creators c JOIN posts p ON p.creator_id = c.id"
	var args []any
	if platform != "" {
		query += " WHERE c.platform = ?"
		args = append(args, platform)
	}
	rows, err := s.index.Query(query+" GROUP BY c.id ORDER BY c.platform, c.name", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Creator
	for rows.Next() {
		var c Creator
		if err := rows.Scan(&c.ID, &c.Platform, &c.Name, &c.Posts); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// Stats 统计作品、缩略图与图集入库情况，供管理页显示进度。
type Stats struct {
	Videos         int `json:"videos"`
	Albums         int `json:"albums"`
	Thumbnails     int `json:"thumbnails"`
	ThumbFailures  int `json:"thumbnail_failures"`
	AlbumsImported int `json:"albums_imported"`
	AlbumsOnDisk   int `json:"albums_on_disk"`
	// AlbumsCleanable 是已入库但原目录仍在、可校验后移入回收站的图集数。
	AlbumsCleanable int `json:"albums_cleanable"`
}

// Stats 汇总当前库的状态。
func (s *Store) Stats() (Stats, error) {
	var st Stats
	err := s.index.QueryRow(`SELECT
		count(*) FILTER (WHERE kind='video'),
		count(*) FILTER (WHERE kind='album'),
		count(*) FILTER (WHERE kind='album' AND imported=1),
		count(*) FILTER (WHERE kind='album' AND on_disk=1),
		count(*) FILTER (WHERE kind='album' AND imported=1 AND on_disk=1)
		FROM posts`).Scan(&st.Videos, &st.Albums, &st.AlbumsImported, &st.AlbumsOnDisk, &st.AlbumsCleanable)
	if err != nil {
		return st, err
	}
	err = s.thumbs.QueryRow("SELECT count(*) FILTER (WHERE failed=0), count(*) FILTER (WHERE failed=1) FROM thumbnails").Scan(&st.Thumbnails, &st.ThumbFailures)
	return st, err
}

func (f Filter) clause() (string, []any) {
	var conds []string
	var args []any
	if f.CreatorID != "" {
		conds = append(conds, "creator_id = ?")
		args = append(args, f.CreatorID)
	} else if f.Platform != "" {
		// creator_id 以 "平台/" 开头；用范围比较以走索引并避免 LIKE 通配符转义。
		conds = append(conds, "creator_id >= ? AND creator_id < ?")
		args = append(args, f.Platform+"/", f.Platform+"0") // '0' 紧随 '/' 之后
	}
	if f.Kind != "" {
		conds = append(conds, "kind = ?")
		args = append(args, string(f.Kind))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// postSignatures 读取全部作品的变化判定字段，供增量扫描比较。
func (s *Store) postSignatures() (map[string]Post, error) {
	rows, err := s.index.Query("SELECT " + postColumns + " FROM posts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := make(map[string]Post)
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		known[p.ID] = p
	}
	return known, rows.Err()
}

// writePosts 在一个事务内写入一个博主的新增/变化作品。
// 图集内容变化时清除入库标记，需重新入库后才允许清理原目录。
func (s *Store) writePosts(posts []scannedPost, known map[string]Post) error {
	if len(posts) == 0 {
		return nil
	}
	tx, err := s.index.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO posts (` + postColumns + `) VALUES (?,?,?,?,?,?,?,?,?,1,?)
		ON CONFLICT(id) DO UPDATE SET creator_id=excluded.creator_id, kind=excluded.kind, title=excluded.title,
		published=excluded.published, size=excluded.size, mtime=excluded.mtime, image_count=excluded.image_count,
		music=excluded.music, on_disk=1, imported=excluded.imported`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range posts {
		old := known[p.ID]
		imported := old.Imported && old.Size == p.Size && old.MTime == p.MTime && old.ImageCount == p.ImageCount
		if _, err := stmt.Exec(p.ID, p.CreatorID, p.Kind, p.Title, p.Published, p.Size, p.MTime, p.ImageCount, p.Music, imported); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// removePosts 删除作品记录及其缩略图；源文件不受影响。
func (s *Store) removePosts(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for _, db := range []*sql.DB{s.index, s.thumbs} {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		query := "DELETE FROM posts WHERE id=?"
		if db == s.thumbs {
			query = "DELETE FROM thumbnails WHERE post_id=?"
		}
		for _, id := range ids {
			if _, err := tx.Exec(query, id); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// replaceCreators 用本次扫描得到的博主集合替换 creators 表。
func (s *Store) replaceCreators(creators map[string]Creator) error {
	tx, err := s.index.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM creators"); err != nil {
		return err
	}
	for _, c := range creators {
		if _, err := tx.Exec("INSERT INTO creators (id, platform, name) VALUES (?,?,?)", c.ID, c.Platform, c.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}
