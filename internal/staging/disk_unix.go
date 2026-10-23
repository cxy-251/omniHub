//go:build !windows

package staging

import (
	"os"
	"syscall"
)

// SameDisk 判断两个路径是否在同一个文件系统上（同盘移动只是改名，跨盘要复制）。
func SameDisk(a, b string) bool {
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	da, okA := sa.Sys().(*syscall.Stat_t)
	db, okB := sb.Sys().(*syscall.Stat_t)
	return okA && okB && da.Dev == db.Dev
}
