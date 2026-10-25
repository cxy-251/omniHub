package gallery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"omnihub/internal/storage"
)

// 一个资源库的相册目录（media_library/gallery）下可以有多个图库：
//   根目录本身（原来的相册，Key 为空）
//   每个子文件夹一个图库（如 X、Twice），各自有 gallery.db（索引与缩略图）和 originals/ 原图分卷
// 子文件夹里可放 collection.json：{"name": "显示名", "nsfw": true}。
// 只放了原图分卷（表 originals(id, data)，id 是原图 SHA-256）的图库，由 IndexVolumes 在后台建索引后装载。

// CollectionFile 是图库说明文件名。
const CollectionFile = "collection.json"

// indexedFile 记下建过索引的分卷（名字与大小），分卷有变化时再补。
const indexedFile = ".indexed.json"

// Collection 是一个图库。
type Collection struct {
	Key        string `json:"key"`  // 根目录为空，否则是子文件夹名
	Name       string `json:"name"` // 显示名
	NSFW       bool   `json:"nsfw"`
	Indexed    bool   `json:"indexed"`     // 有 gallery.db，可以浏览
	NeedsIndex bool   `json:"needs_index"` // 有没建过索引的原图分卷
	Dir        string `json:"-"`
}

type collectionInfo struct {
	Name string `json:"name"`
	NSFW bool   `json:"nsfw"`
}

// ValidKey 判断图库名能不能用作子文件夹名（不含路径分隔、不以点开头）。
func ValidKey(key string) bool {
	return key != "" && len(key) <= 100 && !strings.ContainsAny(key, `/\`) && !strings.HasPrefix(key, ".") && key == filepath.Base(key)
}

// ReadCollection 读一个图库目录的状态（目录不存在时返回 ok=false）。
func ReadCollection(dir, key string) (Collection, bool) {
	c := Collection{Key: key, Name: key, Dir: dir}
	if key == "" {
		c.Name = "相册"
	}
	if info, ok := readInfo(dir); ok {
		if info.Name != "" {
			c.Name = info.Name
		}
		c.NSFW = info.NSFW
	}
	_, err := os.Stat(filepath.Join(dir, "gallery.db"))
	c.Indexed = err == nil
	vols := volumeFiles(dir, key == "")
	if len(vols) == 0 && !c.Indexed {
		return c, false
	}
	c.NeedsIndex = len(vols) > 0 && !sameVolumes(dir, vols)
	return c, true
}

func readInfo(dir string) (collectionInfo, bool) {
	var info collectionInfo
	data, err := os.ReadFile(filepath.Join(dir, CollectionFile))
	return info, err == nil && json.Unmarshal(data, &info) == nil
}

// MarkedNSFW 判断目录的说明文件是否标了 NSFW。短视频的平台目录（如 shortvideo/X）也用同一个文件。
func MarkedNSFW(dir string) bool {
	info, _ := readInfo(dir)
	return info.NSFW
}

// Collections 列出相册目录下的图库：根目录（有 gallery.db 时）和各子文件夹。
func Collections(galleryRoot string) []Collection {
	var out []Collection
	if c, ok := ReadCollection(galleryRoot, ""); ok && c.Indexed {
		c.NeedsIndex = false // 根目录的相册由导入维护，不重新建索引
		out = append(out, c)
	}
	entries, _ := os.ReadDir(galleryRoot)
	for _, e := range entries {
		if !e.IsDir() || !ValidKey(e.Name()) || e.Name() == "originals" {
			continue
		}
		if c, ok := ReadCollection(filepath.Join(galleryRoot, e.Name()), e.Name()); ok {
			out = append(out, c)
		}
	}
	return out
}

// WriteCollectionInfo 写图库说明（已有时不覆盖）。
func WriteCollectionInfo(dir, name string, nsfw bool) error {
	path := filepath.Join(dir, CollectionFile)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(collectionInfo{Name: name, NSFW: nsfw}, "", "  ")
	return os.WriteFile(path, data, 0o644)
}

type volumeFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// volumeFiles 列出 originals/ 下的原图分卷；子文件夹图库还认直接放在图库目录里的分卷（建索引时挪进 originals/）。
func volumeFiles(dir string, root bool) []volumeFile {
	var out []volumeFile
	add := func(d string) {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			name := e.Name()
			if !e.Type().IsRegular() || !volumeNamePattern.MatchString(name) {
				continue
			}
			if info, err := e.Info(); err == nil {
				out = append(out, volumeFile{name, info.Size()})
			}
		}
	}
	add(filepath.Join(dir, "originals"))
	if !root {
		add(dir)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func sameVolumes(dir string, vols []volumeFile) bool {
	data, err := os.ReadFile(filepath.Join(dir, indexedFile))
	if err != nil {
		return false
	}
	var done []volumeFile
	if json.Unmarshal(data, &done) != nil || len(done) != len(vols) {
		return false
	}
	for i := range vols {
		if vols[i] != done[i] {
			return false
		}
	}
	return true
}

// IndexResult 是建索引的结果。
type IndexResult struct {
	Volumes     int      `json:"volumes"`
	Added       int      `json:"added"`
	Known       int      `json:"known"`
	Unsupported int      `json:"unsupported"`
	Errors      []string `json:"errors,omitempty"`
}

// IndexVolumes 给一个子文件夹图库建索引：直接放在图库目录里的分卷先挪进 originals/（同盘只是改名），
// 再读每个分卷，gallery.db 里还没有的照片算尺寸、主色、缩略图写进去。原图分卷只读、不改动内容。
// progress(已处理, 总数) 每张调用一次。
func IndexVolumes(ctx context.Context, dir string, progress func(done, total int)) (IndexResult, error) {
	var res IndexResult
	originals := filepath.Join(dir, "originals")
	if err := os.MkdirAll(originals, 0o755); err != nil {
		return res, err
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if e.Type().IsRegular() && volumeNamePattern.MatchString(name) {
			if err := os.Rename(filepath.Join(dir, name), filepath.Join(originals, name)); err != nil {
				return res, err
			}
		}
	}
	vols := volumeFiles(dir, true)
	res.Volumes = len(vols)
	meta, err := OpenMetaDB(dir)
	if err != nil {
		return res, err
	}
	defer meta.Close()
	total := 0
	for _, v := range vols {
		n, err := countOriginals(filepath.Join(originals, v.Name))
		if err != nil {
			res.Errors = append(res.Errors, v.Name+"："+err.Error())
			continue
		}
		total += n
	}
	done := 0
	for _, v := range vols {
		path := filepath.Join(originals, v.Name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		db, err := storage.OpenSQLite(path, true)
		if err != nil {
			res.Errors = append(res.Errors, v.Name+"："+err.Error())
			continue
		}
		err = indexVolume(ctx, meta, db, v.Name, info.ModTime(), &res, func() {
			done++
			if progress != nil {
				progress(done, total)
			}
		})
		db.Close()
		if err != nil {
			return res, err
		}
	}
	if len(res.Errors) == 0 {
		data, _ := json.Marshal(vols)
		if err := os.WriteFile(filepath.Join(dir, indexedFile), data, 0o644); err != nil {
			return res, err
		}
	}
	return res, nil
}

func countOriginals(path string) (int, error) {
	db, err := storage.OpenSQLite(path, true)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	return n, db.QueryRow("SELECT COUNT(*) FROM originals").Scan(&n)
}

func indexVolume(ctx context.Context, meta *MetaDB, db *sql.DB, volume string, created time.Time, res *IndexResult, step func()) error {
	// 先取 ID 再逐张读原图，不一次把整卷读进内存
	rows, err := db.QueryContext(ctx, "SELECT id FROM originals")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		step()
		var exists int
		if err := meta.db.QueryRow("SELECT COUNT(*) FROM photos WHERE id = ?", id).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			res.Known++
			continue
		}
		var data []byte
		if err := db.QueryRowContext(ctx, "SELECT data FROM originals WHERE id = ?", id).Scan(&data); err != nil {
			res.fail(volume, id, err)
			continue
		}
		d, err := describe(data)
		if errors.Is(err, errUnsupported) {
			res.Unsupported++
			continue
		}
		if err != nil {
			res.fail(volume, id, err)
			continue
		}
		if err := insertPhoto(meta, id, id, volume, len(data), d, created); err != nil {
			return err
		}
		res.Added++
	}
	return nil
}

func (r *IndexResult) fail(volume, id string, err error) {
	if len(r.Errors) < 20 {
		r.Errors = append(r.Errors, volume+"/"+id+"："+err.Error())
	}
}
