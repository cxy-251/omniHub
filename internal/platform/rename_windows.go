//go:build windows

package platform

import "golang.org/x/sys/windows"

// RenameNoReplace 移动应用状态目录，不设置替换标志，因此目标存在时失败。
func RenameNoReplace(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}
