package games

import (
	"context"
	"errors"
	"log"
	"math"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// Two-level available-memory thresholds from the game runtime specification.
const (
	WarningThresholdBytes = 1200 * 1024 * 1024
	KillThresholdBytes    = 500 * 1024 * 1024
)

// ReadMemAvailableBytes reads current OS available memory without disk scanning.
func ReadMemAvailableBytes() (int64, error) {
	metrics, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	if metrics.Total == 0 || metrics.Available > metrics.Total || metrics.Available > math.MaxInt64 {
		return 0, errors.New("invalid system memory metrics")
	}
	return int64(metrics.Available), nil
}

// WatchdogCallbacks reports one warning per pressure episode and a forced-stop
// request after a grace period. Errors are reported once until a successful read.
type WatchdogCallbacks struct {
	OnWarning func(availBytes int64)
	OnKilled  func(availBytes int64)
	OnError   func(error)
	OnTick    func() // 每次采样后调用（硬件画像）
}

// StartWatchdog watches the retained game identity, stopping on completion or
// cancellation. It never signals a numeric PID after reuse and caps shutdown
// grace at one second when available memory is critically low.
func StartWatchdog(ctx context.Context, game *LaunchedGame, interval time.Duration, cb WatchdogCallbacks) error {
	if game == nil || game.group == nil {
		return errors.New("watchdog requires an owned game process")
	}
	if interval <= 0 {
		interval = time.Second
	}
	go watchMemory(ctx, game, interval, cb, ReadMemAvailableBytes)
	return nil
}

func watchMemory(ctx context.Context, game *LaunchedGame, interval time.Duration, cb WatchdogCallbacks, read func() (int64, error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	warned, failed := false, false
	report := func(err error) {
		if cb.OnError != nil {
			cb.OnError(err)
		} else {
			log.Printf("Game watchdog: %v", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-game.done:
			return
		case <-ticker.C:
			if cb.OnTick != nil {
				cb.OnTick()
			}
			available, err := read()
			if err == nil && available < 0 {
				err = errors.New("invalid available memory")
			}
			if err != nil {
				if !failed {
					report(err)
				}
				failed = true
				continue
			}
			failed = false
			if available < KillThresholdBytes {
				if err := game.Stop(false); err != nil {
					report(err)
				}
				timer := time.NewTimer(time.Second)
				select {
				case <-game.done:
					timer.Stop()
					return
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
					if err := game.Stop(true); err != nil {
						report(err)
						return
					}
					if cb.OnKilled != nil {
						cb.OnKilled(available)
					}
					return
				}
			}
			if available < WarningThresholdBytes {
				if !warned && cb.OnWarning != nil {
					cb.OnWarning(available)
				}
				warned = true
			} else if available >= WarningThresholdBytes+(128<<20) {
				warned = false
			}
		}
	}
}
