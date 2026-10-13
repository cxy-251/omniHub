//go:build windows

package platform

import "golang.org/x/sys/windows"

// SyncDirectory 在 Windows 由移动操作的 WRITE_THROUGH 同步目录元数据。
func SyncDirectory(path string) error { return nil }

// ReplaceFile 替换应用拥有的配置文件，并同步移动操作。
func ReplaceFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
