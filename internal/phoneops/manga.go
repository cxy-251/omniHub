package phoneops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const ChunkSize int64 = 8 << 20

type File struct {
	Rel      string `json:"rel"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Hash     string `json:"hash,omitempty"`
	Complete bool   `json:"complete,omitempty"`
	Recycled bool   `json:"recycled,omitempty"`
}
type MangaPlan struct {
	ID           string `json:"id"`
	Device       string `json:"device"`
	Identity     string `json:"identity"`
	LibraryID    string `json:"library_id"`
	SourceName   string `json:"source_name"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	Reserve      int64  `json:"reserve"`
	RemoveSource bool   `json:"remove_source"`
	Files        []File `json:"files"`
	Total        int64  `json:"total"`
	Needed       int64  `json:"needed"`
	Free         int64  `json:"free"`
}
type Progress struct {
	Phase      string `json:"phase"`
	File       string `json:"file"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	Bytes      int64  `json:"bytes"`
	TotalBytes int64  `json:"total_bytes"`
}

// Catalogue includes only regular CBZ books inside the standard manga directory.
// Rejecting symlinks rather than following them also protects subsequent recycling.
func Catalogue(root string) ([]File, error) {
	if err := localPath(root, ""); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []File{}, nil
		}
		return nil, err
	}
	files := []File{}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("普通漫画目录含符号链接，请先移开：%s", d.Name())
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".cbz") {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("不是普通文件：%s", d.Name())
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if !validRel(rel) {
			return errors.New("漫画文件名含不支持的路径或控制字符")
		}
		folded := strings.ToLower(rel)
		if seen[folded] {
			return fmt.Errorf("手机不区分大小写，存在同名漫画：%s", rel)
		}
		seen[folded] = true
		files = append(files, File{Rel: rel, Size: info.Size(), Modified: info.ModTime().UnixNano()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, err
}
func validRel(rel string) bool {
	return rel != "" && rel != "." && !path.IsAbs(rel) && path.Clean(rel) == rel && !strings.HasPrefix(rel, "../") && !strings.ContainsAny(rel, "\x00\n\r\t\\")
}
func localPath(root, rel string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("来源路径不规范")
	}
	current := root
	for {
		st, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return errors.New("来源目录包含符号链接或非目录")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if rel == "" {
		return nil
	}
	if !validRel(rel) {
		return errors.New("来源路径越界")
	}
	current = root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("来源路径含符号链接")
		}
		if i < len(parts)-1 && !st.IsDir() {
			return errors.New("来源父目录不可用")
		}
	}
	return nil
}
func unchanged(root string, f File) (*os.File, error) {
	if err := localPath(root, f.Rel); err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(root, filepath.FromSlash(f.Rel)))
	if err != nil {
		return nil, err
	}
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() != f.Size || st.ModTime().UnixNano() != f.Modified {
		file.Close()
		return nil, errors.New("漫画在预检后发生变化，请重新检查")
	}
	return file, nil
}
func localHash(ctx context.Context, f *os.File, n int64) (string, error) {
	h := sha256.New()
	reader := io.NewSectionReader(f, 0, n)
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		k, err := reader.Read(buf)
		if k > 0 {
			h.Write(buf[:k])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func destination(p *MangaPlan, f File) string {
	return path.Join(p.Target, "media_library/manga/standard", f.Rel)
}
func partial(p *MangaPlan, f File) string {
	return path.Join(p.Target, ".omnihub-manga-transfer", p.ID, Token(f.Rel)+".part")
}

// Preview reads sizes only. Completed same-size files are still hash-checked by RunManga.
func Preview(ctx context.Context, r Remote, p *MangaPlan) error {
	p.Total = 0
	p.Needed = 0
	for _, f := range p.Files {
		if !validRel(f.Rel) {
			return errors.New("非法漫画相对路径")
		}
		p.Total += f.Size
		rel := path.Dir(strings.TrimPrefix(destination(p, f), p.Target+"/"))
		if _, err := r.Shell(ctx, SafeDirs(p.Target, rel, false)); err != nil {
			return err
		}
		n, err := RemoteSize(ctx, r, destination(p, f))
		if err != nil {
			return err
		}
		if n >= 0 && n != f.Size {
			return fmt.Errorf("手机上有同名但大小不同的漫画，不覆盖：%s", f.Rel)
		}
		if n == f.Size {
			continue
		}
		if _, err = r.Shell(ctx, SafeDirs(p.Target, path.Dir(strings.TrimPrefix(partial(p, f), p.Target+"/")), false)); err != nil {
			return err
		}
		n, err = RemoteSize(ctx, r, partial(p, f))
		if err != nil {
			return err
		}
		if n < 0 || n > f.Size {
			n = 0
		}
		p.Needed += f.Size - n
	}
	var err error
	p.Free, err = Free(ctx, r, p.Target)
	if err != nil {
		return err
	}
	if p.Free-p.Needed < p.Reserve {
		return fmt.Errorf("空间不足：剩余 %.2f GiB，本次还需 %.2f GiB，预留 %.1f GiB", float64(p.Free)/(1<<30), float64(p.Needed)/(1<<30), float64(p.Reserve)/(1<<30))
	}
	return nil
}

// RunManga verifies partial prefixes and whole files, then publishes without overwriting.
// checkpoint must durably persist the plan before any source is recycled.
func RunManga(ctx context.Context, r Remote, p *MangaPlan, checkpoint func(*MangaPlan) error, progress func(Progress), trash func(string) error) error {
	if len(p.ID) != 32 || p.Reserve < 0 || !validAbsolute(p.Target) {
		return errors.New("迁移计划无效")
	}
	if err := Preview(ctx, r, p); err != nil {
		return err
	}
	var doneBytes int64
	emit := func(phase string, f File, i int, n int64) {
		if progress != nil {
			progress(Progress{phase, f.Rel, i, len(p.Files), doneBytes + n, p.Total})
		}
	}
	for i := range p.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := &p.Files[i]
		f := *item
		emit("校验本机漫画", f, i, 0)
		dst := destination(p, f)
		if f.Recycled {
			h, err := RemoteHash(ctx, r, dst)
			if err != nil || h != f.Hash {
				return fmt.Errorf("已迁移漫画的手机副本不可用：%s", f.Rel)
			}
			doneBytes += f.Size
			emit("已完成", f, i+1, 0)
			continue
		}
		file, err := unchanged(p.Source, f)
		// A crash can happen after trash succeeds but before its checkpoint.
		// Only recover a previously committed file, and verify the phone again.
		if errors.Is(err, os.ErrNotExist) && f.Complete && p.RemoveSource && len(f.Hash) == 64 {
			h, e := RemoteHash(ctx, r, dst)
			if e != nil || h != f.Hash {
				return fmt.Errorf("来源已不在原位，手机副本校验失败：%s", f.Rel)
			}
			item.Recycled = true
			if e = checkpoint(p); e != nil {
				return e
			}
			doneBytes += f.Size
			emit("已完成", f, i+1, 0)
			continue
		}
		if err != nil {
			return fmt.Errorf("%s：%w", f.Rel, err)
		}
		hash, err := localHash(ctx, file, f.Size)
		if err != nil {
			file.Close()
			return err
		}
		if f.Hash != "" && f.Hash != hash {
			file.Close()
			return fmt.Errorf("来源内容变化：%s", f.Rel)
		}
		item.Hash = hash
		err = transferFile(ctx, r, p, *item, file, func(phase string, n int64) { emit(phase, f, i, n) })
		if err == nil {
			st, e := file.Stat()
			if e != nil || st.Size() != f.Size || st.ModTime().UnixNano() != f.Modified {
				err = errors.New("传输期间来源文件发生变化")
			}
		}
		file.Close()
		if err != nil {
			return fmt.Errorf("%s：%w", f.Rel, err)
		}
		item.Complete = true
		if err = checkpoint(p); err != nil {
			return err
		}
		if p.RemoveSource {
			emit("回收本机已校验副本", f, i, f.Size)
			// Reopen the named path and rehash immediately before moving it to trash.
			current, err := unchanged(p.Source, *item)
			if err != nil {
				return err
			}
			h, err := localHash(ctx, current, f.Size)
			current.Close()
			if err != nil || h != hash {
				return errors.New("回收前来源变化，保留本机文件")
			}
			remoteHash, err := RemoteHash(ctx, r, dst)
			if err != nil || remoteHash != hash {
				return errors.New("回收前手机副本不可用，保留本机文件")
			}
			if trash == nil {
				return errors.New("系统回收站不可用")
			}
			if err = trash(filepath.Join(p.Source, filepath.FromSlash(f.Rel))); err != nil {
				return err
			}
			item.Recycled = true
			if err = checkpoint(p); err != nil {
				return err
			}
		}
		doneBytes += f.Size
		emit("已完成", f, i+1, 0)
	}
	return nil
}
func transferFile(ctx context.Context, r Remote, p *MangaPlan, f File, file *os.File, emit func(string, int64)) error {
	dst, tmp := destination(p, f), partial(p, f)
	dirScript := SafeDirs(p.Target, path.Dir(strings.TrimPrefix(dst, p.Target+"/")), true) + SafeDirs(p.Target, path.Dir(strings.TrimPrefix(tmp, p.Target+"/")), true)
	if _, err := r.Shell(ctx, dirScript); err != nil {
		return err
	}
	size, err := RemoteSize(ctx, r, dst)
	if err != nil {
		return err
	}
	if size >= 0 {
		h, err := RemoteHash(ctx, r, dst)
		if err != nil {
			return err
		}
		if size != f.Size || h != f.Hash {
			return errors.New("手机已有同名但内容不同的文件，未覆盖")
		}
		emit("手机已存在，校验通过", f.Size)
		return nil
	}
	offset, err := RemoteSize(ctx, r, tmp)
	if err != nil {
		return err
	}
	if offset < 0 {
		offset = 0
	}
	if offset > 0 {
		emit("校验断点", offset)
		remote, err := RemoteHash(ctx, r, tmp)
		if err != nil {
			return err
		}
		local, err := localHash(ctx, file, min(offset, f.Size))
		if err != nil {
			return err
		}
		if offset > f.Size || remote != local {
			return errors.New("未完成文件的断点校验失败，请重新建立迁移计划")
		}
	}
	for offset < f.Size {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(ChunkSize, f.Size-offset)
		free, err := Free(ctx, r, p.Target)
		if err != nil {
			return err
		}
		if free-n < p.Reserve {
			return errors.New("手机空间不足，已保留断点；释放空间后继续")
		}
		script := dirScript + "test ! -L " + Quote(tmp) + " || exit 40\n" + fmt.Sprintf("test \"$(stat -c %%s %s 2>/dev/null || echo 0)\" = %d || exit 42\ncat >> %s", Quote(tmp), offset, Quote(tmp))
		emit("正在传输", offset)
		if err = r.Append(ctx, script, io.NewSectionReader(file, offset, n)); err != nil {
			return fmt.Errorf("连接中断或写入失败；断点已保留，可重新连接后继续：%w", err)
		}
		offset += n
		actual, err := RemoteSize(ctx, r, tmp)
		if err != nil {
			return err
		}
		if actual != offset {
			return errors.New("手机写入长度不一致；断点已保留")
		}
		emit("正在传输", offset)
	}
	// Empty files need a staging inode too.
	if f.Size == 0 {
		if _, err = r.Shell(ctx, dirScript+"test ! -L "+Quote(tmp)+" && touch "+Quote(tmp)); err != nil {
			return err
		}
	}
	emit("核对手机完整文件", f.Size)
	hash, err := RemoteHash(ctx, r, tmp)
	if err != nil {
		return err
	}
	if hash != f.Hash {
		return errors.New("手机完整文件校验失败，未发布")
	}
	_, err = r.Shell(ctx, dirScript+"test ! -L "+Quote(dst)+" && test ! -e "+Quote(dst)+" && mv -n "+Quote(tmp)+" "+Quote(dst)+" && test ! -e "+Quote(tmp))
	if err != nil {
		return fmt.Errorf("目标出现同名文件或发布失败，未覆盖：%w", err)
	}
	hash, err = RemoteHash(ctx, r, dst)
	if err != nil {
		return err
	}
	if hash != f.Hash {
		return errors.New("发布后校验失败")
	}
	return nil
}
