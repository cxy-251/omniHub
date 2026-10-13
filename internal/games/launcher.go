package games

import (
	"context"
	"fmt"
	"log"
	"omnihub/internal/platform"
	"os"
	"path/filepath"
	"time"
)

// LaunchOptions 封装游戏拉起参数。
type LaunchOptions struct {
	GameDir string // 游戏物理所在绝对目录
	ExePath string // 可执行程序相对路径
	Args    []string
	Env     []string
	UserID  int64  // 当前启动游戏的用户 ID
	IsAdmin bool   // 是否为管理员
	LogsDir string // 日志存放根目录
}

// LaunchedGame 记录已启动的游戏进程信息。
type LaunchedGame struct {
	PID       int
	PGID      int
	LogPath   string
	StartTime time.Time
}

// PrepareSaveDir 准备多用户隔离存档目录（第 4.4 节）。
// Admin 直通游戏目录原生 save/，普通用户隔离重定向至 save/users/{user_id}/。
func PrepareSaveDir(gameDir string, userID int64, isAdmin bool) (string, error) {
	var targetSave string
	if !isAdmin && userID <= 0 {
		return "", fmt.Errorf("authenticated user required for game saves")
	}
	if isAdmin {
		targetSave = filepath.Join(gameDir, "save")
	} else {
		targetSave = filepath.Join(gameDir, "save", "users", fmt.Sprintf("%d", userID))
	}

	if err := os.MkdirAll(targetSave, 0755); err != nil {
		return "", fmt.Errorf("failed to create save directory: %w", err)
	}
	return targetSave, nil
}

// LaunchGame 拉起游戏子进程，执行脱钩、I/O 压制与日志重定向（第 4.3 节）。
func LaunchGame(ctx context.Context, opt LaunchOptions) (*LaunchedGame, error) {
	if opt.GameDir == "" || !filepath.IsLocal(opt.ExePath) || opt.LogsDir == "" {
		return nil, fmt.Errorf("invalid game launch parameters")
	}

	fullExe := filepath.Join(opt.GameDir, opt.ExePath)
	if _, err := os.Stat(fullExe); err != nil {
		return nil, fmt.Errorf("executable not found: %s", fullExe)
	}

	// 准备日志落盘文件（第 4.3.3 节）
	if err := os.MkdirAll(opt.LogsDir, 0755); err != nil {
		return nil, fmt.Errorf("create game logs: %w", err)
	}
	logName := fmt.Sprintf("%s_%d.log", filepath.Base(opt.GameDir), time.Now().Unix())
	logPath := filepath.Join(opt.LogsDir, logName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}

	// 准备存档目录
	saveDir, err := PrepareSaveDir(opt.GameDir, opt.UserID, opt.IsAdmin)
	if err != nil {
		_ = logFile.Close()
		return nil, err
	}

	// 构造命令并通过 ionice -c 3 压制 I/O 抢占（第 4.3.1 节）
	cmd := platform.BackgroundCommand(ctx, fullExe, opt.Args...)

	cmd.Dir = opt.GameDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// 注入多用户环境变量
	env := os.Environ()
	env = append(env, fmt.Sprintf("OMNI_SAVE_DIR=%s", saveDir))
	cmd.Env = append(env, opt.Env...)

	if err := platform.Start(cmd, true); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("failed to start game process: %w", err)
	}
	pgid := cmd.Process.Pid
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("Game %d exited: %v", pgid, err)
		}
		if err := platform.Stop(pgid, true); err != nil {
			log.Printf("Clean game descendants: %v", err)
		}
		if err := platform.Release(pgid); err != nil {
			log.Printf("Release game process: %v", err)
		}
		if err := logFile.Close(); err != nil {
			log.Printf("Close game log: %v", err)
		}
	}()

	return &LaunchedGame{
		PID:       cmd.Process.Pid,
		PGID:      pgid,
		LogPath:   logPath,
		StartTime: time.Now(),
	}, nil
}
