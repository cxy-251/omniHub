package platform

import (
	"errors"
	"fmt"
	"syscall"
)

func assertChildInactive(pid int) error {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return fmt.Errorf("descendant %d still exists", pid)
}
