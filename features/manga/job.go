// Package mangajob runs the offline manga packer as an on-demand uv/Python job.
package mangajob

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"omnihub/internal/scanner"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"omnihub/internal/manga"
	"omnihub/internal/platform"
	"omnihub/internal/tasks"
)

//go:embed scripts/pack.py
var packScript string

// Definitions register shipped pack/index jobs; no interpreter starts at boot.
func Definitions(configDir string) ([]tasks.Definition, error) {
	configDir, err := filepath.Abs(configDir)
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	pack := tasks.Definition{ID: "manga.pack", Name: "漫画图片目录封箱（CBZ）", Category: "media_library/manga", Write: true, LockCategory: true, Executable: executable, Args: []string{"--manga-pack"}, Parameters: []tasks.Parameter{{Name: "output", Label: "输出文件名（.cbz）", Required: true}}, Env: map[string]string{"UV_CACHE_DIR": filepath.Join(configDir, "task-cache", "uv")}}
	index := tasks.Definition{ID: "manga.index", Name: "漫画 CBZ 索引更新", Category: pack.Category, Write: true, LockCategory: true, Executable: executable, Args: []string{"--manga-index"}, Parameters: []tasks.Parameter{}}
	return []tasks.Definition{pack, index}, nil
}

// Run executes the embedded Python source through existing uv/interpreters,
// offline with no dependency installation. Descendants inherit the owned job.
func Run(output string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if err := validOutput(output); err != nil {
		return err
	}
	if root := os.Getenv("OMNIHUB_TASK_CATEGORY_DIR"); root != "" {
		directory, err := os.Getwd()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, directory)
		if err != nil || !filepath.IsLocal(relative) || strings.ContainsAny(relative, "\\\x00:") {
			return errors.New("select a portable directory within the manga category")
		}
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		return errors.New("漫画封箱需要已有 uv 和 Python >=3.10；不会自动安装")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// Keep this group leader alive while Python handles TERM and removes its own
	// temporary file. The manager escalates the entire group after five seconds.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	command := exec.Command(uv, "run", "--offline", "--no-config", "--no-project", "--no-python-downloads", "--python", ">=3.10", "python", "-I", "-S", "-", output)
	command.Env = append(os.Environ(), "OMNIHUB_TASK_PUBLISHER="+executable)
	command.Stdin = strings.NewReader(packScript)
	// Wrapping forces exec to drain inherited pipes through Python cleanup even
	// when uv exits first on TERM; direct *os.File inheritance would not wait.
	command.Stdout = struct{ io.Writer }{stdout}
	command.Stderr = struct{ io.Writer }{stderr}
	if err = command.Run(); err != nil {
		return fmt.Errorf("漫画封箱失败：%w", err)
	}
	return nil
}

func validOutput(name string) error {
	for _, character := range name {
		if character < 32 || character == 127 {
			return errors.New("输出文件名含控制字符")
		}
	}
	if name == "" || len(name) > 240 || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\\x00\r\n<>:\"|?*") || !strings.EqualFold(filepath.Ext(name), ".cbz") || filepath.Base(name) != name {
		return errors.New("输出必须是当前目录内的普通 .cbz 文件名")
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
		return errors.New("输出文件名使用了系统保留名称")
	}
	return nil
}

// Publish validates a completed job-owned temporary archive, then publishes in
// the current directory atomically without replacing an existing file or link.
func Publish(temporary, output string) error {
	if err := validOutput(output); err != nil {
		return err
	}
	if filepath.Base(temporary) != temporary || !strings.HasPrefix(temporary, ".omnihub-pack-") || !strings.HasSuffix(temporary, ".tmp") || len(temporary) > 128 || strings.ContainsAny(temporary, "/\\\x00") {
		return errors.New("invalid pack temporary filename")
	}
	info, err := os.Lstat(temporary)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("pack temporary must be a regular file")
	}
	reader, err := manga.OpenCBZ(temporary)
	if err != nil {
		return err
	}
	_, pages, err := reader.PageWindow(0, 1)
	if err == nil && pages == 0 {
		err = errors.New("archive has no reading pages")
	}
	cover := reader.GetCoverEntry("")
	if err == nil && cover != "cover.jpg" && cover != "cover.webp" {
		err = errors.New("archive requires a root cover.jpg or cover.webp")
	}
	if err == nil {
		err = reader.ValidateCoverEntryContext(context.Background(), cover)
	}
	if err == nil {
		var metadata *manga.PackageMetadata
		metadata, err = reader.ParseMetadata()
		if err == nil && strings.TrimSpace(metadata.Title) == "" {
			err = errors.New("archive metadata title is required")
		}
	}
	if err = errors.Join(err, reader.Close()); err != nil {
		return err
	}
	// Relative basenames preserve the inherited cwd identity and cannot select
	// another directory. Publication uses the platform exclusive-rename operation.
	latest, err := os.Lstat(temporary)
	if err != nil {
		return err
	}
	if !os.SameFile(info, latest) || info.Size() != latest.Size() || !info.ModTime().Equal(latest.ModTime()) {
		return errors.New("temporary archive changed during validation")
	}
	if err = platform.RenameNoReplace(temporary, output); err != nil {
		return fmt.Errorf("publish without replacement: %w", err)
	}
	if err = platform.SyncDirectory("."); err != nil {
		return fmt.Errorf("archive published but directory sync failed: %w", err)
	}
	if category := os.Getenv("OMNIHUB_TASK_CATEGORY_DIR"); category != "" {
		file, err := filepath.Abs(output)
		if err != nil {
			return fmt.Errorf("archive published but index path failed: %w", err)
		}
		if err = scanner.IndexMangaFile(context.Background(), category, file); err != nil {
			return fmt.Errorf("archive published but index failed: %w", err)
		}
	}
	return nil
}

// RunIndex refreshes an explicitly selected subtree under the trusted category.
func RunIndex(stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	root := os.Getenv("OMNIHUB_TASK_CATEGORY_DIR")
	if root == "" {
		return errors.New("index task requires a resolved manga category")
	}
	scope, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if _, err = fmt.Fprintln(stdout, "INFO scanning CBZ metadata; existing history is preserved"); err != nil {
		return err
	}
	count, err := scanner.ScanMangaContext(ctx, root, scope)
	if err != nil {
		return fmt.Errorf("indexed %d archives before failure: %w", count, err)
	}
	_, err = fmt.Fprintf(stdout, "INFO indexed %d archives\n", count)
	return err
}
