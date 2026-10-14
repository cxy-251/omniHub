//go:build linux

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func assertChildInactive(pid int) error {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 || len(data) < end+3 {
		return fmt.Errorf("invalid child stat")
	}
	// PID 1 may briefly retain an exited orphan zombie; it has no execution/RSS.
	if data[end+2] != 'Z' && data[end+2] != 'X' {
		return fmt.Errorf("descendant %d remains active", pid)
	}
	return nil
}
