package games

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchdogCoalescesWarningEpisodesAndReadFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	game := &LaunchedGame{done: make(chan struct{})}
	var warnings, failures atomic.Int32
	calls := 0
	read := func() (int64, error) {
		calls++
		switch calls {
		case 1, 2, 4:
			return WarningThresholdBytes - 1, nil
		case 3:
			return WarningThresholdBytes + (128 << 20), nil
		case 5, 6:
			return 0, errors.New("unavailable metrics")
		default:
			cancel()
			return WarningThresholdBytes + (256 << 20), nil
		}
	}
	done := make(chan struct{})
	go func() {
		watchMemory(ctx, game, time.Millisecond, WatchdogCallbacks{OnWarning: func(int64) { warnings.Add(1) }, OnError: func(error) { failures.Add(1) }}, read)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop on cancellation")
	}
	if warnings.Load() != 2 || failures.Load() != 1 {
		t.Fatalf("warning/read-error flood: %d/%d", warnings.Load(), failures.Load())
	}
	if err := StartWatchdog(ctx, nil, time.Second, WatchdogCallbacks{}); err == nil {
		t.Fatal("unowned watchdog accepted")
	}
}
