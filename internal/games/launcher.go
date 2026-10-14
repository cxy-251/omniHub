package games

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"omnihub/internal/platform"
)

// LaunchOptions selects a validated native executable relative to GameDir.
// Args are argv elements, not shell text; Env cannot override the save boundary.
type LaunchOptions struct {
	GameDir string
	ExePath string
	Args    []string
	Env     []string
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

func validateLaunch(opt LaunchOptions) (path string, result error) {
	if opt.GameDir == "" || opt.LogsDir == "" || !filepath.IsLocal(opt.ExePath) || len(opt.ExePath) > 4096 || len(opt.Args) > 128 || len(opt.Env) > 128 {
		return "", errors.New("invalid game launch parameters")
	}
	if !opt.IsAdmin && opt.UserID <= 0 {
		return "", errors.New("authenticated user required")
	}
	total := 0
	for _, arg := range opt.Args {
		total += len(arg)
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return "", errors.New("invalid game argument")
		}
	}
	for _, env := range opt.Env {
		total += len(env)
		key, _, ok := strings.Cut(env, "=")
		if !ok || len(env) > 4096 || strings.ContainsRune(env, 0) || key == "" || strings.EqualFold(key, "OMNI_SAVE_DIR") {
			return "", errors.New("invalid game environment")
		}
		for i, c := range key {
			if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
				return "", errors.New("invalid environment name")
			}
		}
	}
	if total > 32<<10 {
		return "", errors.New("game arguments/environment exceed capacity")
	}
	info, err := os.Lstat(opt.GameDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("game root must be a real directory")
	}
	root, err := os.OpenRoot(opt.GameDir)
	if err != nil {
		return "", err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	info, err = root.Lstat(opt.ExePath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("game executable must be a regular file")
	}
	file, err := root.Open(opt.ExePath)
	if err != nil {
		return "", err
	}
	opened, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(statErr, closeErr); err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", errors.New("executable changed during validation")
	}
	if runtime.GOOS != "windows" {
		if strings.EqualFold(filepath.Ext(opt.ExePath), ".exe") {
			return "", errors.New("Windows executable requires a configured Wine/Proton driver")
		}
		if info.Mode().Perm()&0111 == 0 {
			return "", errors.New("game executable is not executable")
		}
	}
	return filepath.Join(opt.GameDir, opt.ExePath), nil
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
	executable, err := validateLaunch(opt)
	if err != nil {
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
	cmd := platform.BackgroundCommand(context.Background(), executable, opt.Args...)
	cmd.Dir = opt.GameDir
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.WaitDelay = time.Second
	// Canonical save path is last; untrusted overrides were rejected above.
	cmd.Env = append(append(os.Environ(), opt.Env...), "OMNI_SAVE_DIR="+saveDir)
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, logFile.Close())
	}
	if err := platform.Start(cmd, true); err != nil {
		return nil, errors.Join(err, logFile.Close())
	}
	game := &LaunchedGame{PID: cmd.Process.Pid, PGID: cmd.Process.Pid, LogPath: logFile.Name(), StartTime: time.Now(), done: make(chan struct{})}
	go func() {
		// Never kill a Unix process group after Wait reaps its leader: its numeric
		// ID can have been reused. Windows Release acts on the retained Job handle.
		game.result = errors.Join(cmd.Wait(), output.flush(), logFile.Close(), platform.Release(game.PID))
		close(game.done)
	}()
	return game, nil
}

var _ io.Writer = (*gameLog)(nil)
