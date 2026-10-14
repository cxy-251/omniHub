//go:build linux || darwin

package platform

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
)

// ProcessGroup owns one unreaped group leader. Callers must use Wait instead
// of cmd.Wait; Stop is fenced before the leader can be reaped or its PID reused.
// Descendants remaining in the group are killed when the leader exits.
type ProcessGroup struct {
	mu      sync.Mutex
	once    sync.Once
	cmd     *exec.Cmd
	pid     int
	retired bool
	result  error
}

// StartGroup starts a dedicated process group/session and retains its identity.
func StartGroup(cmd *exec.Cmd, detached bool) (*ProcessGroup, error) {
	if cmd == nil {
		return nil, ErrInvalidProcessGroup
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: detached, Setpgid: !detached}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &ProcessGroup{cmd: cmd, pid: cmd.Process.Pid}, nil
}

// PID returns the original leader identifier for display and metrics, not signals.
func (g *ProcessGroup) PID() int {
	if g == nil {
		return 0
	}
	return g.pid
}

// Stop sends TERM/KILL only while the original leader remains unreaped.
// A retired group is a no-op, including late timeout callbacks.
func (g *ProcessGroup) Stop(force bool) error {
	if g == nil || g.cmd == nil || g.pid <= 0 {
		return ErrInvalidProcessGroup
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.retired {
		return nil
	}
	return signalOwnedGroup(g.pid, force)
}

func signalOwnedGroup(pid int, force bool) error {
	if pid <= 0 {
		return ErrInvalidProcessGroup
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	err := syscall.Kill(-pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// Wait observes exit without reaping, retires and cleans the group, optionally
// drains caller-owned output, then reaps. beforeReap must not wait indefinitely.
// Concurrent/repeated callers receive the same result; only the first hook runs.
func (g *ProcessGroup) Wait(beforeReap func() error) error {
	if g == nil || g.cmd == nil || g.pid <= 0 {
		return ErrInvalidProcessGroup
	}
	g.once.Do(func() {
		observeErr := observeChildExit(g.pid)
		g.mu.Lock()
		g.retired = true
		var cleanupErr error
		// If ownership was lost or waitid failed, never signal the numeric group.
		if observeErr == nil {
			cleanupErr = signalOwnedGroup(g.pid, true)
		}
		g.mu.Unlock()
		var drainErr error
		if beforeReap != nil {
			drainErr = beforeReap()
		}
		g.result = errors.Join(observeErr, cleanupErr, drainErr, g.cmd.Wait())
	})
	return g.result
}
