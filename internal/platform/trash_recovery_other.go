//go:build !linux

package platform

import (
	"errors"
	"os"
)

// RestoreTrashReceipt leaves restoration to the OS UI on other platforms.
func RestoreTrashReceipt(receipt TrashReceipt, library *os.Root, relative string) error {
	return errors.New("请在系统回收站恢复此文件")
}
