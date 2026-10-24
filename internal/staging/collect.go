package staging

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BrowserPlatforms 是短视频插件（ExtensionForge/005）在 Chrome 下载目录里建的平台文件夹。
var BrowserPlatforms = []string{"抖音", "快手", "TikTok", "小红书"}

// 最近这么久内还在变的作品（文件或图集文件夹里的任一文件）当作还在下载，这次不收。
const settleTime = 10 * time.Minute

// CollectResult 是一次收件的结果。
type CollectResult struct {
	Batch   string         `json:"batch"`
	Moved   int            `json:"moved"`   // 收进来的作品（视频文件或图集文件夹）
	Files   int            `json:"files"`   // 其中的文件数
	Busy    int            `json:"busy"`    // 还在下载、没收的作品
	Loose   int            `json:"loose"`   // 下载目录根下没有作者的视频（插件没拿到文件名时 Chrome 存成「下载 (N).mp4」），没动
	Authors map[string]int `json:"authors"` // 平台/作者 → 收进来的作品数
	Errors  []string       `json:"errors,omitempty"`
}

// CollectBrowser 把 downloads/<平台>/<作者>/ 下插件下好的作品移进资源库 root 的一个新批次，
// 落在 media_library/shortvideo/<平台>/<作者>/。每个作者文件夹里的 downloaded.txt（插件的已下载名单）留在原处
// （插件靠它判断哪些已经下过，移走会让插件以为没下过），同时复制一份进批次，移入资源库时覆盖库里旧的那份。没有可收的内容时返回 ErrNothing、不建批次。
func CollectBrowser(ctx context.Context, downloads, root string, progress func(done, total int)) (CollectResult, error) {
	res := CollectResult{Authors: map[string]int{}}
	type work struct{ platform, author, name string }
	var todo []work
	now := time.Now()
	for _, platform := range BrowserPlatforms {
		authors, _ := os.ReadDir(filepath.Join(downloads, platform))
		for _, a := range authors {
			if !a.IsDir() {
				continue
			}
			entries, _ := os.ReadDir(filepath.Join(downloads, platform, a.Name()))
			for _, e := range entries {
				name := e.Name()
				if name == "downloaded.txt" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".crdownload") {
					continue
				}
				if recent(filepath.Join(downloads, platform, a.Name(), name), now) {
					res.Busy++
					continue
				}
				todo = append(todo, work{platform, a.Name(), name})
			}
		}
	}
	if loose, err := os.ReadDir(downloads); err == nil {
		for _, e := range loose {
			if e.Type().IsRegular() && Kind(e.Name()) == "video" {
				res.Loose++
			}
		}
	}
	if len(todo) == 0 {
		return res, ErrNothing
	}
	batch, err := NewBatch(root, "浏览器插件下载", "chrome:"+downloads)
	if err != nil {
		return res, err
	}
	res.Batch = batch
	base := filepath.Join(root, Dir, batch, "media_library", "shortvideo")
	for i, w := range todo {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		src := filepath.Join(downloads, w.platform, w.author, w.name)
		dst := filepath.Join(base, w.platform, w.author, w.name)
		n, err := moveTree(src, dst)
		res.Files += n
		if err != nil {
			if len(res.Errors) < 20 {
				res.Errors = append(res.Errors, fmt.Sprintf("%s/%s/%s：%v", w.platform, w.author, w.name, err))
			}
		} else {
			res.Moved++
			res.Authors[w.platform+"/"+w.author]++
		}
		if progress != nil {
			progress(i+1, len(todo))
		}
	}
	for author := range res.Authors {
		src := filepath.Join(downloads, filepath.FromSlash(author), RecordFile)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(base, filepath.FromSlash(author), RecordFile)); err != nil && len(res.Errors) < 20 {
				res.Errors = append(res.Errors, author+"/"+RecordFile+"："+err.Error())
			}
		}
	}
	return res, nil
}

// RecordFile 是浏览器插件在每个作者文件夹里记的已下载名单（一行「平台 作品ID」，只增不减）。
const RecordFile = "downloaded.txt"

// copyFile 复制到 dst（先写临时文件再改名，dst 已存在时替换）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".omnihub-part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// recent 判断文件（或文件夹里任一文件）在 settleTime 内改过。
func recent(p string, now time.Time) bool {
	busy := false
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil // 文件夹自己的修改时间不算（新建文件夹时就是当前时间），只看里面的文件
		}
		if strings.HasSuffix(d.Name(), ".crdownload") {
			busy = true
		} else if info, err := d.Info(); err == nil && now.Sub(info.ModTime()) < settleTime {
			busy = true
		}
		if busy {
			return fs.SkipAll
		}
		return nil
	})
	return busy
}

// moveTree 移动一个文件或整个文件夹（图集），返回移动的文件数。目标已存在的文件不覆盖、留在原处。
func moveTree(src, dst string) (int, error) {
	info, err := os.Lstat(src)
	if err != nil {
		return 0, err
	}
	if info.Mode().IsRegular() {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return 0, err
		}
		if err := moveFile(src, dst); err != nil {
			return 0, err
		}
		return 1, nil
	}
	if !info.IsDir() {
		return 0, nil
	}
	moved := 0
	var firstErr error
	_ = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
			err = moveFile(p, target)
			if err == nil {
				moved++
			} else if firstErr == nil {
				firstErr = err
			}
		}
		return nil
	})
	removeEmptyDirs(src)
	_ = os.Remove(src)
	return moved, firstErr
}
