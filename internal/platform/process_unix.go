//go:build !windows

// Package platform isolates operating system process and storage operations.
package platform

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"syscall"
)

// BackgroundCommand lowers CPU priority and, on Linux, I/O priority for background tasks.
func BackgroundCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	if runtime.GOOS == "linux" {
		args = append([]string{"-c", "3", executable}, args...)
		executable = "ionice"
	}
	return exec.CommandContext(ctx, "nice", append([]string{"-n", "10", executable}, args...)...)
}

// Start creates an isolated process group, optionally detached into a new session.
func Start(cmd *exec.Cmd, detached bool) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: detached, Setpgid: !detached}
	return cmd.Start()
}

// Stop signals a process group; an already exited group is treated as success.
func Stop(pid int, force bool) error {
	if pid <= 0 {
		return errors.New("invalid process group ID")
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

// Release releases platform bookkeeping after the child has been reaped.
func Release(pid int) error { return nil }
