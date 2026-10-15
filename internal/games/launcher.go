package games

import (
	"bufio"
	"strings"
	"os/exec"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"omnihub/internal/platform"
)

// LaunchOptions 描述一次游戏启动。Argv 由 Command 生成（原生程序 / Proton / Lime3DS / Ren'Py），
// 不是 shell 文本；WorkDir 为工作目录（主程序所在目录），Env 追加在系统环境之后，不能覆盖存档目录。
type LaunchOptions struct {
	GameDir string
	WorkDir string
	Argv    []string
	Env     []string
	Scope   string // systemd 用户 scope 单元名；为空时不建 scope
	UserID  int64
	IsAdmin bool
	LogsDir string
}

// LaunchedGame identifies a detached process and exposes completion without
// polling. Completion does not authorize sending signals to the old PID.
type LaunchedGame struct {
	PID       int
	PGID      int
	LogPath   string
	StartTime time.Time
	done      chan struct{}
	result    error
	group     *platform.ProcessGroup
	scope     string // systemd 用户 scope；Wine 的子进程各自开新会话，只有 scope 能覆盖全部
}

// Stop requests exit using the retained process identity, never a raw saved PID.
func (g *LaunchedGame) Stop(force bool) error {
	if g == nil || g.group == nil {
		return errors.New("game has no managed process")
	}
	if g.scope != "" {
		signal := "SIGTERM"
		if force {
			signal = "SIGKILL"
		}
		if err := exec.Command("systemctl", "--user", "kill", "--signal="+signal, g.scope).Run(); err == nil {
			return nil
		}
	}
	return g.group.Stop(force)
}

// stopScope 在游戏主进程退出后结束 scope 里残留的进程（wineserver、winedevice 等）。
func stopScope(scope string) {
	if scope != "" {
		_ = exec.Command("systemctl", "--user", "stop", scope).Run()
	}
}

// Wait waits for process/output cleanup; cancelling the wait does not kill it.
func (g *LaunchedGame) Wait(ctx context.Context) error {
	if g == nil || g.done == nil {
		return errors.New("game has no managed process")
	}
	select {
	case <-g.done:
		return g.result
	default:
	}
	select {
	case <-g.done:
		return g.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

// PrepareSaveDir creates save or save/users/ID inside an existing game root.
// Every save component must be a real directory, including internal links.
func PrepareSaveDir(gameDir string, userID int64, isAdmin bool) (path string, result error) {
	if !isAdmin && userID <= 0 {
		return "", errors.New("authenticated user required for game saves")
	}
	root, err := os.OpenRoot(gameDir)
	if err != nil {
		return "", err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	parts := []string{"save"}
	if !isAdmin {
		parts = append(parts, "users", strconv.FormatInt(userID, 10))
	}
	name := ""
	for _, part := range parts {
		name = filepath.Join(name, part)
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(name, 0755); err != nil && !errors.Is(err, os.ErrExist) {
				return "", err
			}
			info, err = root.Lstat(name)
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("save path must contain real directories")
		}
	}
	return filepath.Join(gameDir, name), nil
}

func validateLaunch(opt LaunchOptions) error {
	if opt.GameDir == "" || opt.LogsDir == "" || len(opt.Argv) == 0 || len(opt.Argv) > 160 || len(opt.Env) > 160 {
		return errors.New("invalid game launch parameters")
	}
	if !opt.IsAdmin && opt.UserID <= 0 {
		return errors.New("authenticated user required")
	}
	if err := validateLaunchValues(opt.Argv, opt.Env); err != nil {
		return err
	}
	info, err := os.Lstat(opt.GameDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("game root must be a real directory")
	}
	if !filepath.IsAbs(opt.Argv[0]) {
		return errors.New("launch program must be an absolute path")
	}
	if info, err := os.Stat(opt.Argv[0]); err != nil || !info.Mode().IsRegular() {
		return errors.New("launch program not found")
	}
	return nil
}

// gameEnvironment 返回给游戏用的环境：去掉 omniHub 自身运行所需、会干扰游戏的变量
// （指向本机 Qt 开发环境的库与插件路径等）。Lime3DS 等自带 Qt 的程序被引到错误的 Qt 上会崩溃。
func gameEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case name == "LD_LIBRARY_PATH", name == "QML_IMPORT_PATH", name == "QML2_IMPORT_PATH", name == "LDFLAGS", name == "CGO_ENABLED",
			strings.HasPrefix(name, "QT_"), strings.HasPrefix(name, "QSG_"), strings.HasPrefix(name, "OMNIHUB_"), strings.HasPrefix(name, "OH_"):
			continue
		}
		env = append(env, kv)
	}
	return env
}

// scopedArgv 把命令放进独立的 systemd 用户 scope（独立 cgroup，游戏内存不记在 omniHub 名下，
// 系统 OOM 杀游戏不会波及 omniHub；规格 4.3.1），并以 ionice -c 3 运行以免突发读盘拖死整机。
// systemd-run --scope 会以同一 PID exec 目标命令，进程组管理不受影响。
func scopedArgv(scope string, argv []string) []string {
	if runtime.GOOS != "linux" {
		return argv
	}
	if _, err := exec.LookPath("ionice"); err == nil {
		argv = append([]string{"ionice", "-c", "3"}, argv...)
	}
	if _, err := exec.LookPath("systemd-run"); err == nil && scope != "" {
		argv = append([]string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "--unit=" + scope, "--"}, argv...)
	}
	return argv
}

// gameLog drains output but stores at most 4MiB, in 16KiB batches. It keeps
// the prefix and final 64KiB, reporting consumed bytes even after the disk cap.
type gameLog struct {
	mu        sync.Mutex
	output    *bufio.Writer
	remaining int
	err       error
	truncated bool
	tail      []byte
	flushed   bool
}

const gameLogTailBytes = 64 << 10
const gameLogMarker = "\n[omniHub: middle output omitted; final 64KiB retained at exit]\n"

func (w *gameLog) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	consumed := len(data)
	if w.err != nil || w.flushed {
		return consumed, nil
	}
	discarded := len(data) > w.remaining
	if discarded {
		w.keepTail(data[w.remaining:])
		data = data[:w.remaining]
	}
	if len(data) > 0 {
		n, err := w.output.Write(data)
		w.remaining -= n
		w.err = err
	}
	if discarded && !w.truncated && w.err == nil {
		w.truncated = true
		_, w.err = w.output.WriteString(gameLogMarker)
	}
	return consumed, nil
}

func (w *gameLog) keepTail(data []byte) {
	if cap(w.tail) < gameLogTailBytes {
		w.tail = make([]byte, 0, gameLogTailBytes)
	}
	if len(data) >= gameLogTailBytes {
		w.tail = append(w.tail[:0], data[len(data)-gameLogTailBytes:]...)
		return
	}
	if excess := len(w.tail) + len(data) - gameLogTailBytes; excess > 0 {
		copy(w.tail, w.tail[excess:])
		w.tail = w.tail[:len(w.tail)-excess]
	}
	w.tail = append(w.tail, data...)
}
func (w *gameLog) flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.flushed {
		return w.err
	}
	w.flushed = true
	if w.err == nil && len(w.tail) > 0 {
		_, w.err = w.output.Write(w.tail)
	}
	w.err = errors.Join(w.err, w.output.Flush())
	return w.err
}

// LaunchGame validates and starts a native game. ctx governs startup only;
// an HTTP response or GUI disconnect cannot cancel the detached game. Logs use
// exclusive random names/0600 permissions. Root-owner save semantics still need
// engine-specific drivers: OMNI_SAVE_DIR alone does not redirect arbitrary games.
func LaunchGame(ctx context.Context, opt LaunchOptions) (*LaunchedGame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateLaunch(opt); err != nil {
		return nil, err
	}
	saveDir, err := PrepareSaveDir(opt.GameDir, opt.UserID, opt.IsAdmin)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opt.LogsDir, 0755); err != nil {
		return nil, err
	}
	info, err := os.Lstat(opt.LogsDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("logs must use a real directory")
	}
	pattern := filepath.Base(opt.GameDir) + "_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_*.log"
	logFile, err := os.CreateTemp(opt.LogsDir, pattern)
	if err != nil {
		return nil, err
	}
	output := &gameLog{output: bufio.NewWriterSize(logFile, 16<<10), remaining: (4 << 20) - len(gameLogMarker) - gameLogTailBytes}
	argv := scopedArgv(opt.Scope, opt.Argv)
	scope := ""
	if len(argv) > 0 && argv[0] == "systemd-run" {
		scope = opt.Scope + ".scope"
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opt.WorkDir
	if cmd.Dir == "" {
		cmd.Dir = opt.GameDir
	}
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.WaitDelay = time.Second
	// Canonical save path is last; untrusted overrides were rejected above.
	cmd.Env = append(append(gameEnvironment(), opt.Env...), "OMNI_SAVE_DIR="+saveDir)
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, logFile.Close())
	}
	group, err := platform.StartGroup(cmd, true)
	if err != nil {
		return nil, errors.Join(err, logFile.Close())
	}
	game := &LaunchedGame{PID: group.PID(), PGID: group.PID(), LogPath: logFile.Name(), StartTime: time.Now(), done: make(chan struct{}), group: group, scope: scope}
	go func() {
		waitErr := group.Wait(nil)
		stopScope(scope)
		game.result = errors.Join(waitErr, output.flush(), logFile.Close())
		close(game.done)
	}()
	return game, nil
}

var _ io.Writer = (*gameLog)(nil)
