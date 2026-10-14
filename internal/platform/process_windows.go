package platform

import (
	"context"
	"os/exec"
)

// BackgroundCommand leaves priority/group setup to the owned Windows Job starter.
func BackgroundCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, executable, args...)
}
