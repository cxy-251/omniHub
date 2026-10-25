package shortvideo

import (
	"bufio"
	"bytes"
	"context"
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

// ArchiveFile 是下载器在博主目录里记录已下载作品的清单（每行一个作品），同步时两边合并而不是覆盖。
const ArchiveFile = "downloaded.txt"

// partSuffix 是同步时正在写入的临时文件后缀；写完、落盘后才改成正式文件名。
const partSuffix = ".omnihub-part"

// CopyPlan 是把一个短视频目录增量补到另一个目录的计划（ID 都是相对 shortvideo/ 的斜杠路径）。
type CopyPlan struct {
	Files     []CopyFile `json:"-"`
	Count     int        `json:"files"`
	Bytes     int64      `json:"bytes"`
	Archives  []string   `json:"archives"`  // 要合并进目标的 downloaded.txt
	Back      []string   `json:"back"`      // 目标里有、来源缺记录的 downloaded.txt：合并回来源，让来源的记录保持完整
	Creators  []string   `json:"creators"`  // 有内容要补的博主
	Conflicts []string   `json:"conflicts"` // 两边同名、大小不同的文件：不覆盖，只报告
	Skipped   int        `json:"skipped"`   // 目标里没有的博主（只补已有博主时）
	Prefixes  []string   `json:"-"`         // 处理的博主（"平台/博主/"），合并已入库图集时按它筛选
}

// CopyFile 是一个要复制的文件。
type CopyFile struct {
	Rel  string
	Size int64
}

// CopyResult 是执行结果。
type CopyResult struct {
	Copied   int   `json:"copied"`
	Bytes    int64 `json:"bytes"`
	Archives int   `json:"archives"` // 合并后有新增行的 downloaded.txt
	Back     int   `json:"back"`     // 合并回来源的 downloaded.txt
}

// PlanCopy 比较 src 与 dst 两个 shortvideo/ 目录，列出 dst 缺少的文件。
// existingOnly 时只处理 dst 里已有的博主；否则 src 的博主全部补过去，平台目录下的文件（如 collection.json）也补。
// 规则：dst 没有的文件复制；同名同大小视为已有；downloaded.txt 两边合并；同名不同大小的只报告。
// 下载记录是浏览器插件判断「下过没有」的依据，src 要保存全部记录：dst 所有博主的 downloaded.txt 里 src 没有的行
// 也合并回 src（src 没有这个博主时只建目录放 downloaded.txt，扫描不会把它当成博主）。
// 不复制数据库、隐藏文件、下载中的半截文件和软链接，也从不删除 dst 的任何东西。
func PlanCopy(src, dst string, existingOnly bool) (CopyPlan, error) {
	return planCopy(src, localTarget(dst), existingOnly)
}

// target 是同步的目标一端：本地目录，或经数据线读到的手机清单。rel 都是相对 shortvideo/ 的斜杠路径。
type target interface {
	isDir(rel string) bool
	size(rel string) (int64, bool)
	records(rel string) ([]string, error) // 某个 downloaded.txt 的记录行（没有时为空）
	recordFiles() ([]string, error)       // 所有 <平台>/<博主>/downloaded.txt
}

type localTarget string

func (t localTarget) path(rel string) string {
	return filepath.Join(string(t), filepath.FromSlash(rel))
}
func (t localTarget) isDir(rel string) bool {
	st, err := os.Stat(t.path(rel))
	return err == nil && st.IsDir()
}
func (t localTarget) size(rel string) (int64, bool) {
	st, err := os.Stat(t.path(rel))
	if err != nil {
		return 0, false
	}
	return st.Size(), true
}
func (t localTarget) records(rel string) ([]string, error) { return archiveLines(t.path(rel)) }
func (t localTarget) recordFiles() ([]string, error) {
	found, err := filepath.Glob(filepath.Join(string(t), "*", "*", ArchiveFile))
	for i, f := range found {
		r, _ := filepath.Rel(string(t), f)
		found[i] = filepath.ToSlash(r)
	}
	return found, err
}

func planCopy(src string, dst target, existingOnly bool) (CopyPlan, error) {
	var plan CopyPlan
	platforms, err := os.ReadDir(src)
	if err != nil {
		return plan, err
	}
	creators := map[string]bool{}
	for _, p := range platforms {
		if skipName(p.Name()) {
			continue
		}
		if p.Type().IsRegular() {
			if !existingOnly {
				plan.add(src, dst, p.Name(), creators)
			}
			continue
		}
		if !p.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(src, p.Name()))
		if err != nil {
			return plan, err
		}
		for _, c := range entries {
			rel := p.Name() + "/" + c.Name()
			if skipName(c.Name()) {
				continue
			}
			if !c.IsDir() {
				if c.Type().IsRegular() && !existingOnly {
					plan.add(src, dst, rel, creators)
				}
				continue
			}
			if existingOnly {
				if !dst.isDir(rel) {
					plan.Skipped++
					continue
				}
			}
			plan.Prefixes = append(plan.Prefixes, rel+"/")
			err := filepath.WalkDir(filepath.Join(src, filepath.FromSlash(rel)), func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if skipName(d.Name()) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if d.Type().IsRegular() {
					r, _ := filepath.Rel(src, path)
					plan.add(src, dst, filepath.ToSlash(r), creators)
				}
				return nil
			})
			if err != nil {
				return plan, err
			}
		}
	}
	back, err := dst.recordFiles()
	if err != nil {
		return plan, err
	}
	for _, rel := range back {
		theirs, err := dst.records(rel)
		if err != nil {
			return plan, err
		}
		have, err := archiveLines(filepath.Join(src, filepath.FromSlash(rel)))
		if err != nil {
			return plan, err
		}
		if len(linesMissing(theirs, have)) > 0 {
			plan.Back = append(plan.Back, rel)
		}
	}
	for c := range creators {
		if c != "" {
			plan.Creators = append(plan.Creators, c)
		}
	}
	sort.Strings(plan.Creators)
	plan.Count = len(plan.Files)
	return plan, nil
}

func skipName(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".ytdl") || strings.HasSuffix(name, partSuffix) {
		return true
	}
	for _, db := range DatabaseFiles {
		if name == db || strings.HasPrefix(name, db+"-") {
			return true
		}
	}
	return false
}

// add 比较一个文件，按规则记进计划。
func (plan *CopyPlan) add(src string, dst target, rel string, creators map[string]bool) {
	from := filepath.Join(src, filepath.FromSlash(rel))
	st, err := os.Stat(from)
	if err != nil {
		return
	}
	creator := "" // 平台目录下的文件不属于博主
	if parts := strings.SplitN(rel, "/", 3); len(parts) == 3 {
		creator = parts[0] + "/" + parts[1]
	}
	have, ok := dst.size(rel)
	switch {
	case !ok:
		plan.Files = append(plan.Files, CopyFile{rel, st.Size()})
		plan.Bytes += st.Size()
		creators[creator] = true
	case path.Base(rel) == ArchiveFile:
		mine, _ := archiveLines(from)
		theirs, _ := dst.records(rel)
		if len(linesMissing(mine, theirs)) > 0 {
			plan.Archives = append(plan.Archives, rel)
			creators[creator] = true
		}
	case have != st.Size():
		plan.Conflicts = append(plan.Conflicts, rel)
	}
}

// RunCopy 按计划复制文件、合并 downloaded.txt。每个文件先写成临时文件并落盘，大小核对无误再改名，
// 中断只会留下临时文件（下次同步时覆盖重写），不会出现半截的正式文件。progress 在每个文件完成后调用。
func RunCopy(ctx context.Context, src, dst string, plan CopyPlan, progress func(done int, bytes int64)) (CopyResult, error) {
	var res CopyResult
	for _, rel := range plan.Back {
		if _, err := MergeArchive(filepath.Join(dst, filepath.FromSlash(rel)), filepath.Join(src, filepath.FromSlash(rel))); err != nil {
			return res, fmt.Errorf("%s：%w", rel, err)
		}
		res.Back++
	}
	for _, f := range plan.Files {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := copyFile(ctx, filepath.Join(src, filepath.FromSlash(f.Rel)), filepath.Join(dst, filepath.FromSlash(f.Rel))); err != nil {
			return res, fmt.Errorf("%s：%w", f.Rel, err)
		}
		res.Copied++
		res.Bytes += f.Size
		if progress != nil {
			progress(res.Copied, res.Bytes)
		}
	}
	for _, rel := range plan.Archives {
		added, err := MergeArchive(filepath.Join(src, filepath.FromSlash(rel)), filepath.Join(dst, filepath.FromSlash(rel)))
		if err != nil {
			return res, fmt.Errorf("%s：%w", rel, err)
		}
		if added > 0 {
			res.Archives++
		}
	}
	return res, nil
}

// copyFile 复制一个文件到不存在的目标路径（目标已存在则不动它，返回错误）。
func copyFile(ctx context.Context, from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return errors.New("目标已存在")
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	tmp := to + partSuffix
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, ctxReader{ctx, in})
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != st.Size() {
		err = fmt.Errorf("只写入 %d / %d 字节", n, st.Size())
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Chtimes(tmp, st.ModTime(), st.ModTime())
	if _, err := os.Lstat(to); err == nil {
		os.Remove(tmp)
		return errors.New("目标已存在")
	}
	return os.Rename(tmp, to)
}

// ctxReader 让长时间的复制能被取消。
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// linesMissing 返回 theirs 里有、have 里没有的行（按原顺序，去重）。
func linesMissing(theirs, have []string) []string {
	seen := map[string]bool{}
	for _, l := range have {
		seen[l] = true
	}
	var missing []string
	for _, l := range theirs {
		if !seen[l] {
			seen[l] = true
			missing = append(missing, l)
		}
	}
	return missing
}

// archiveLines 读出记录行；文件不存在时为空。
func archiveLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, sc.Err()
}

// MergeArchive 把 from 里 to 没有的行追加到 to 的内容后面（to 不存在时新建），写临时文件再替换；返回新增行数。
func MergeArchive(from, to string) (int, error) {
	theirs, err := archiveLines(from)
	if err != nil {
		return 0, err
	}
	return mergeLines(theirs, to)
}

// mergeLines 把 theirs 里 to 没有的行追加到 to 后面（to 不存在时新建）。
func mergeLines(theirs []string, to string) (int, error) {
	have, err := archiveLines(to)
	if err != nil {
		return 0, err
	}
	missing := linesMissing(theirs, have)
	if len(missing) == 0 {
		return 0, nil
	}
	data, err := os.ReadFile(to)
	if errors.Is(err, fs.ErrNotExist) {
		data, err = nil, os.MkdirAll(filepath.Dir(to), 0o755)
	}
	if err != nil {
		return 0, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, strings.Join(missing, "\n")+"\n"...)
	tmp := to + partSuffix
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return 0, err
	}
	f, err := os.Open(tmp)
	if err == nil {
		err = f.Sync()
		f.Close()
	}
	if err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return len(missing), os.Rename(tmp, to)
}

// Keeps 判断一个作品 ID 属于计划处理的博主。
func (plan CopyPlan) Keeps(postID string) bool {
	for _, p := range plan.Prefixes {
		if strings.HasPrefix(postID, p) {
			return true
		}
	}
	return false
}
