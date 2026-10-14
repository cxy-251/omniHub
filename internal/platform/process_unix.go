//go:build !windows

// Package platform isolates operating system process and storage operations.
package platform

import (
	"context"
	"os/exec"
	"runtime"
)

// BackgroundCommand lowers CPU priority and, on Linux, I/O priority for background tasks.
func BackgroundCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	if runtime.GOOS == "linux" {
		args = append([]string{"-c", "3", executable}, args...)
		executable = "ionice"
	}
	return exec.CommandContext(ctx, "nice", append([]string{"-n", "10", executable}, args...)...)
}
