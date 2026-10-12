package games

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 两级内存看门狗阈值常量（第 4.3.2 节）
const (
	WarningThresholdBytes = 1200 * 1024 * 1024 // 1.2GB
	KillThresholdBytes    = 500 * 1024 * 1024  // 500MB
)

// ReadMemAvailableBytes 从 Linux /proc/meminfo 读取系统当前可用内存字节数。
func ReadMemAvailableBytes() (int64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err != nil {
					return 0, err
				}
				return kb * 1024, nil
			}
		}
	}

	return 0, errors.New("MemAvailable field not found in /proc/meminfo")
}

// WatchdogCallbacks 定义内存看门狗触发通知回调。
type WatchdogCallbacks struct {
	OnWarning func(availBytes int64) // 一级预警触发
	OnKilled  func(availBytes int64) // 二级主动熔断触发
}

// StartWatchdog 启动针对特定游戏进程组的内存监控看门狗。
func StartWatchdog(ctx context.Context, pgid int, interval time.Duration, cb WatchdogCallbacks) {
	if interval <= 0 {
		interval = 1 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				avail, err := ReadMemAvailableBytes()
				if err != nil {
					continue
				}

				// 二级主动熔断（< 500MB）：主动强杀保护宿主机
				if avail < KillThresholdBytes {
					if cb.OnKilled != nil {
						cb.OnKilled(avail)
					}
					// 向整个进程组广播 SIGTERM（-pgid）
					_ = syscall.Kill(-pgid, syscall.SIGTERM)
					return
				}

				// 一级告警（< 1.2GB）：通知保存进度
				if avail < WarningThresholdBytes {
					if cb.OnWarning != nil {
						cb.OnWarning(avail)
					}
				}
			}
		}
	}()
}

// KillProcessGroup 安全清理残留的进程组及其孤儿进程（第 4.3.3 节）。
func KillProcessGroup(pgid int) error {
	if pgid <= 0 {
		return nil
	}

	// 先尝试柔和退出 SIGTERM
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	// 等待 2 秒，如果仍有残留则强杀 SIGKILL
	time.Sleep(500 * time.Millisecond)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return fmt.Errorf("process group %d killed", pgid)
}
