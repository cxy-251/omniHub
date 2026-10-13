package games

import (
	"context"
	"errors"
	"github.com/shirou/gopsutil/v4/mem"
	"omnihub/internal/platform"
	"time"
)

// 两级内存看门狗阈值常量（第 4.3.2 节）
const (
	WarningThresholdBytes = 1200 * 1024 * 1024 // 1.2GB
	KillThresholdBytes    = 500 * 1024 * 1024  // 500MB
)

// ReadMemAvailableBytes 通过平台适配读取系统当前可用内存字节数。
func ReadMemAvailableBytes() (int64, error) {
	metrics, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return int64(metrics.Available), nil
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
					_ = platform.Stop(pgid, false)
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

	if err := platform.Stop(pgid, false); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return errors.Join(platform.Stop(pgid, true), platform.Release(pgid))
}
