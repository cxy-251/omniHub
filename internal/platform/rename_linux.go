//go:build linux

package platform

import "golang.org/x/sys/unix"

// RenameNoReplace 原子移动应用状态目录，目标存在时失败，绝不覆盖目标内容。
func RenameNoReplace(source, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}
