package phoneops

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"omnihub/internal/platform"
)

// ProjectRoot accepts an omniHub source checkout, never a packaged desktop binary.
func ProjectRoot(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("请选择本机 omniHub 项目目录")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", errors.New("项目目录缺少 go.mod")
	}
	if !bytes.HasPrefix(module, []byte("module omnihub\n")) && !bytes.HasPrefix(module, []byte("module omnihub\r\n")) {
		return "", errors.New("请选择 omniHub 源码目录")
	}
	for _, rel := range []string{"cmd/server/main.go", "web/package.json", "web/pnpm-lock.yaml", "packaging/android/omnihub.sh"} {
		st, e := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if e != nil || !st.Mode().IsRegular() {
			return "", fmt.Errorf("项目缺少 %s", rel)
		}
	}
	return root, nil
}

// DefaultProject searches beside the running server and its launch directory.
func DefaultProject() string {
	exe, _ := os.Executable()
	cwd, _ := os.Getwd()
	for _, base := range []string{filepath.Dir(exe), cwd} {
		for i := 0; i < 4; i++ {
			if root, err := ProjectRoot(base); err == nil {
				return root
			}
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}
	return ""
}

// BuildTools reports the local toolchain without installing host dependencies.
func BuildTools() map[string]bool {
	found := map[string]bool{}
	for _, name := range []string{"go", "pnpm", "node"} {
		_, err := buildTool(name)
		found[name] = err == nil
	}
	return found
}

// Desktop launchers often omit the user-installed toolchain from PATH.
func buildTool(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	paths := []string{filepath.Join(home, ".local", "bin", name)}
	if name == "go" {
		paths = append(paths, filepath.Join(home, ".local", "go", "bin", "go"), filepath.Join(home, "Applications", "go", "bin", "go"))
	}
	for _, p := range paths {
		if resolved, err := exec.LookPath(p); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("本机缺少 %s，请安装后重试", name)
}

// buildLog bounds individual lines and streams command output into the job log.
type buildLog struct {
	mu      sync.Mutex
	pending []byte
	note    func(string)
}

func (w *buildLog) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range b {
		if c == '\n' || c == '\r' {
			// Vite lists every font/chunk on success; keep phase/error messages visible.
			if s := strings.TrimSpace(string(w.pending)); s != "" && !strings.HasPrefix(s, "dist/") {
				w.note(s)
			}
			w.pending = w.pending[:0]
		} else if len(w.pending) < 4096 {
			w.pending = append(w.pending, c)
		}
	}
	return len(b), nil
}
func (w *buildLog) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if s := strings.TrimSpace(string(w.pending)); s != "" {
		w.note(s)
	}
	w.pending = w.pending[:0]
}
func runBuild(ctx context.Context, root, program string, args, env []string, note func(string)) error {
	// nice/ionice are supplied by the shared platform launcher; cap build parallelism below.
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		args = append([]string{"-n", "19", program}, args...)
		program = "nice"
	}
	cmd := platform.BackgroundCommand(context.Background(), program, args...)
	cmd.Dir = root
	cmd.Env = env
	log := &buildLog{note: note}
	cmd.Stdout = log
	cmd.Stderr = log
	group, err := platform.StartGroup(cmd, false)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = group.Stop(true) })
	err = group.Wait(nil)
	stop()
	log.flush()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func buildEnv(values map[string]string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := values[key]; !replace {
			env = append(env, entry)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

// ValidateServer checks the phone executable before any upload or service stop.
func ValidateServer(file string) error {
	f, err := elf.Open(file)
	if err != nil {
		return errors.New("服务端不是有效的 ELF 可执行文件")
	}
	defer f.Close()
	if f.Machine != elf.EM_AARCH64 || f.Class != elf.ELFCLASS64 {
		return errors.New("服务端架构不是手机所需的 arm64")
	}
	return nil
}

// BuildServer builds the current local Web and Go sources into a Termux server.
// It neither downloads releases nor uploads to a device. Output is task-owned staging.
func BuildServer(ctx context.Context, project, dst string, note func(string)) error {
	root, err := ProjectRoot(project)
	if err != nil {
		return err
	}
	tools := map[string]string{}
	toolDirs := []string{}
	for _, name := range []string{"go", "pnpm", "node"} {
		p, e := buildTool(name)
		if e != nil {
			return e
		}
		tools[name] = p
		toolDirs = append(toolDirs, filepath.Dir(p))
	}
	toolDirs = append(toolDirs, os.Getenv("PATH"))
	values := map[string]string{"PATH": strings.Join(toolDirs, string(os.PathListSeparator))}
	webEnv := buildEnv(values)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err = os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	note("构建来源：" + root + "（包含尚未提交的本机改动）")
	if _, e := os.Stat(filepath.Join(root, "web/node_modules/.modules.yaml")); os.IsNotExist(e) {
		note("安装 Web 构建依赖")
		if err = runBuild(ctx, root, tools["pnpm"], []string{"--dir", "web", "install", "--frozen-lockfile"}, webEnv, note); err != nil {
			return fmt.Errorf("Web 依赖安装失败：%w", err)
		}
	}
	note("正在构建 Web 客户端")
	if err = runBuild(ctx, root, tools["pnpm"], []string{"--dir", "web", "build"}, webEnv, note); err != nil {
		return fmt.Errorf("Web 构建失败，手机原服务未改动：%w", err)
	}
	revision := "local"
	if git, e := exec.LookPath("git"); e == nil {
		cmd := platform.BackgroundCommand(ctx, git, "-C", root, "describe", "--tags", "--always", "--dirty")
		if out, e := cmd.Output(); e == nil {
			value := strings.TrimSpace(string(out))
			if regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,128}$`).MatchString(value) {
				revision = value
			}
		}
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	flags := "-s -w -X omnihub/internal/buildinfo.Revision=" + revision + " -X omnihub/internal/buildinfo.BuildTime=" + stamp
	note("正在构建手机服务端：" + revision + " · " + stamp)
	values["GOOS"], values["GOARCH"], values["CGO_ENABLED"], values["GOMAXPROCS"] = "android", "arm64", "0", "2"
	if err = runBuild(ctx, root, tools["go"], []string{"build", "-p", "2", "-trimpath", "-ldflags", flags, "-o", filepath.Join(dst, "omnihub-server"), "./cmd/server"}, buildEnv(values), note); err != nil {
		return fmt.Errorf("服务端构建失败，手机原服务未改动：%w", err)
	}
	if err = ValidateServer(filepath.Join(dst, "omnihub-server")); err != nil {
		return err
	}
	in, err := os.Open(filepath.Join(root, "packaging/android/omnihub.sh"))
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(dst, "omnihub.sh"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	note("本机服务端构建完成，已内嵌最新 Web；准备传送到手机")
	return nil
}
