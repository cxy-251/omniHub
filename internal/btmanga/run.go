package btmanga

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bufio"
	"omnihub/internal/archiveutil"
	"omnihub/internal/manga"
	"omnihub/internal/ordering"
	"omnihub/internal/platform"
	"sort"
	"strconv"
)

// Book groups one or several releases into one user-named CBZ.
type Book struct {
	Title   string   `json:"title"`
	Sources []string `json:"sources"`
}

// Plan is written by the trusted host, never interpreted as a shell command.
type Plan struct {
	Proxy       string `json:"proxy,omitempty"`
	Aria2       string `json:"aria2"`
	Work        string `json:"work"`
	Destination string `json:"destination"`
	Books       []Book `json:"books"`
}

// Tool finds a user-installed tool; nothing is downloaded or bundled.
func Tool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, p := range []string{filepath.Join(home, "Applications", name), filepath.Join(home, "Applications", strings.TrimSuffix(name, "c"), name)} {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && (st.Mode()&0111 != 0 || filepath.Ext(p) == ".exe") {
			return p
		}
	}
	return ""
}

// Filename rejects path syntax instead of silently changing the requested name.
func Filename(title string) (string, error) {
	title = strings.TrimSpace(strings.TrimSuffix(title, ".cbz"))
	if title == "" || len(title) > 180 || strings.HasPrefix(title, ".") || strings.ContainsAny(title, "/\\:*?\"<>|\x00\r\n\t") || strings.HasSuffix(title, ".") {
		return "", errors.New("请填写不含路径或特殊符号的漫画书名")
	}
	return title + ".cbz", nil
}

// Run downloads serially and verifies every page before publishing without overwrite.
// Source volumes and unfinished downloads are retained for retry/recovery.
func Run(ctx context.Context, p Plan, out io.Writer) error {
	if len(p.Books) == 0 || len(p.Books) > 50 || p.Aria2 == "" || !filepath.IsAbs(p.Work) || !filepath.IsAbs(p.Destination) {
		return errors.New("无效的漫画下载计划")
	}
	for bi, b := range p.Books {
		name, err := Filename(b.Title)
		if err != nil {
			return err
		}
		dest := filepath.Join(p.Destination, name)
		if _, err = os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("目标已存在或不可访问：%s，不会覆盖", name)
		}
		key := sha256.Sum256([]byte(strings.Join(b.Sources, "\n") + "\x00" + name))
		dir := filepath.Join(p.Work, hex.EncodeToString(key[:12]))
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		fmt.Fprintf(out, "INFO %s：下载目录 %s（中断后可用同样的来源与书名继续）\n", b.Title, dir)
		for si, src := range b.Sources {
			fmt.Fprintf(out, "OMNIHUB_PROGRESS %d %d %s · 下载来源 %d/%d\n", bi, len(p.Books), b.Title, si+1, len(b.Sources))
			args := []string{"--no-conf=true", "--enable-rpc=false", "--seed-time=0", "--bt-stop-timeout=180", "--connect-timeout=20", "--timeout=30", "--max-tries=3", "--continue=true", "--auto-file-renaming=false", "--allow-overwrite=false", "--check-integrity=true", "--max-concurrent-downloads=1", "--file-allocation=none", "--summary-interval=5", "--console-log-level=notice", "--download-result=full", "--dir=" + dir}
			if p.Proxy != "" {
				args = append(args, "--all-proxy="+p.Proxy)
			}
			args = append(args, "--", src)
			cmd := exec.CommandContext(ctx, p.Aria2, args...)
			cmd.Stdout = out
			cmd.Stderr = out
			if err = cmd.Run(); err != nil {
				return fmt.Errorf("%s 下载未完成，保留断点：%w", b.Title, err)
			}
		}
		fmt.Fprintf(out, "OMNIHUB_PROGRESS %d %d %s · 合并并校验 CBZ\n", bi, len(p.Books), b.Title)
		if err = Pack(ctx, dir, dest, out); err != nil {
			return fmt.Errorf("%s 封箱失败，保留原分卷：%w", b.Title, err)
		}
		fmt.Fprintf(out, "INFO 已入库 %s；下载分卷仍保留在 %s\n", dest, dir)
		fmt.Fprintf(out, "OMNIHUB_PROGRESS %d %d 已完成 %s\n", bi+1, len(p.Books), b.Title)
		data, _ := json.Marshal(map[string]any{"path": dest, "work": dir})
		fmt.Fprintf(out, "OMNIHUB_RESULT %s\n", data)
	}
	return nil
}

type page struct {
	name string
	size uint64
	open func() (io.ReadCloser, error)
}

// Pack flattens volumes into numbered chapter directories; it never deletes input.
// RAR/CBR is read through an optional 7z, one image at a time, without extraction.
func Pack(ctx context.Context, dir, dest string, out io.Writer) error {
	var sources []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("下载内容含符号链接，拒绝封箱")
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if manga.ImageExtensions[ext] || ext == ".cbz" || ext == ".zip" || ext == ".cbr" || ext == ".rar" {
			sources = append(sources, p)
		}
		if len(sources) > manga.MaxPages {
			return errors.New("下载文件过多")
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(sources, func(i, j int) bool { return ordering.NaturalLess(sources[i], sources[j]) })
	if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dest), ".bt-manga-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	w := zip.NewWriter(f)
	count := 0
	for vi, src := range sources {
		var pages []page
		var closeSource func() error
		ext := strings.ToLower(filepath.Ext(src))
		switch {
		case ext == ".zip" || ext == ".cbz":
			z, err := archiveutil.Open(src)
			if err != nil {
				return err
			}
			closeSource = z.Close
			for _, zf := range z.File {
				if !zf.FileInfo().IsDir() && manga.ImageExtensions[strings.ToLower(filepath.Ext(zf.Name))] {
					pages = append(pages, page{zf.Name, zf.UncompressedSize64, zf.Open})
				}
			}
		case ext == ".rar" || ext == ".cbr":
			pages, err = rarPages(ctx, src)
			if err != nil {
				return err
			}
		default:
			st, err := os.Stat(src)
			if err != nil {
				return err
			}
			pages = []page{{filepath.Base(src), uint64(st.Size()), func() (io.ReadCloser, error) { return os.Open(src) }}}
		}
		sort.Slice(pages, func(i, j int) bool { return ordering.NaturalLess(pages[i].name, pages[j].name) })
		for pi, pg := range pages {
			if ctx.Err() != nil {
				if closeSource != nil {
					closeSource()
				}
				return ctx.Err()
			}
			if pg.size == 0 || pg.size > 32<<20 {
				if closeSource != nil {
					closeSource()
				}
				return errors.New("漫画页为空或超过 32 MiB")
			}
			count++
			if count > manga.MaxPages {
				if closeSource != nil {
					closeSource()
				}
				return fmt.Errorf("漫画超过 %d 页", manga.MaxPages)
			}
			r, err := pg.open()
			if err != nil {
				if closeSource != nil {
					closeSource()
				}
				return err
			}
			h := &zip.FileHeader{Name: fmt.Sprintf("Vol_%04d/%06d%s", vi+1, pi+1, strings.ToLower(filepath.Ext(pg.name))), Method: zip.Store}
			target, err := w.CreateHeader(h)
			var n int64
			if err == nil {
				n, err = io.Copy(target, io.LimitReader(r, 32<<20+1))
			}
			err = errors.Join(err, r.Close())
			if err != nil || uint64(n) != pg.size {
				if closeSource != nil {
					closeSource()
				}
				return errors.Join(errors.New("分卷读取不完整"), err)
			}
		}
		if closeSource != nil {
			if err = closeSource(); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "INFO 合并 %d/%d：%s（累计 %d 页）\n", vi+1, len(sources), filepath.Base(src), count)
	}
	if count == 0 {
		return errors.New("下载内容没有可合并的漫画图片或 CBZ/ZIP 分卷")
	}
	if err = w.Close(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	reader, err := manga.OpenCBZ(tmp)
	if err != nil {
		return err
	}
	for _, e := range reader.ListImageEntries() {
		var data []byte
		if data, err = reader.ReadEntryContext(ctx, e); err != nil {
			reader.Close()
			return err
		}
		if _, _, err = image.DecodeConfig(bytes.NewReader(data)); err != nil {
			reader.Close()
			return fmt.Errorf("漫画页无法解码：%s：%w", e, err)
		}
	}
	if err = reader.Close(); err != nil {
		return err
	}
	if err = platform.RenameNoReplace(tmp, dest); err != nil {
		return err
	}
	fmt.Fprintf(out, "INFO 校验通过：%d 页，%s\n", count, time.Now().Format(time.RFC3339))
	return nil
}

type commandReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (r *commandReader) Close() error {
	err := r.ReadCloser.Close()
	return errors.Join(err, r.cmd.Wait())
}

func rarPages(ctx context.Context, source string) ([]page, error) {
	tool := Tool("7zz")
	if tool == "" {
		tool = Tool("7z")
	}
	if tool == "" {
		return nil, errors.New("CBR/RAR 需要外部工具 7z；安装后重新开始可继续，下载分卷已保留")
	}
	cmd := exec.CommandContext(ctx, tool, "l", "-slt", "--", source)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	var out []page
	active := false
	name := ""
	var size uint64
	flush := func() {
		if manga.ImageExtensions[strings.ToLower(filepath.Ext(name))] {
			entry := name
			out = append(out, page{entry, size, func() (io.ReadCloser, error) {
				c := exec.CommandContext(ctx, tool, "x", "-so", "-spd", "--", source, entry)
				r, e := c.StdoutPipe()
				if e != nil {
					return nil, e
				}
				if e = c.Start(); e != nil {
					return nil, e
				}
				return &commandReader{r, c}, nil
			}})
		}
	}
	sc := bufio.NewScanner(io.LimitReader(pipe, 16<<20))
	sc.Buffer(make([]byte, 4096), 16384)
	for sc.Scan() {
		s := sc.Text()
		if strings.HasPrefix(s, "----------") {
			active = true
			continue
		}
		if !active {
			continue
		}
		if s == "" {
			flush()
			name = ""
			size = 0
		}
		if v, ok := strings.CutPrefix(s, "Path = "); ok {
			name = v
		}
		if v, ok := strings.CutPrefix(s, "Size = "); ok {
			size, _ = strconv.ParseUint(v, 10, 64)
		}
	}
	flush()
	if err = errors.Join(sc.Err(), pipe.Close(), cmd.Wait()); err != nil {
		return nil, err
	}
	if len(out) > manga.MaxPages {
		return nil, errors.New("分卷条目过多")
	}
	return out, nil
}
