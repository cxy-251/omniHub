//go:build linux

package platform

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// RestoreTrashReceipt restores an app-recorded item into a caller-owned library
// root. RENAME_NOREPLACE atomically rejects overwrite; root-relative opens reject escape.
func RestoreTrashReceipt(receipt TrashReceipt, library *os.Root, relative string) error {
	if !filepath.IsLocal(relative) || filepath.Clean(filepath.Join(library.Name(), relative)) != receipt.Original {
		return errors.New("资源库位置已变化，未恢复文件")
	}
	if err := validateTrashReceipt(receipt); err != nil {
		return err
	}
	trash, err := os.OpenRoot(receipt.Directory)
	if err != nil {
		return err
	}
	defer trash.Close()
	entry, err := trash.Lstat("files/" + receipt.Name)
	if err != nil {
		return err
	}
	if !entry.Mode().IsRegular() {
		return errors.New("回收条目不是普通文件")
	}
	// Recreate removed empty album directories through the retained library root,
	// so even a changed/symlinked parent cannot redirect recovery outside the library.
	if err := library.MkdirAll(filepath.Dir(relative), 0755); err != nil {
		return err
	}
	dst, err := library.Open(filepath.Dir(relative))
	if err != nil {
		return err
	}
	defer dst.Close()
	src, err := trash.Open("files")
	if err != nil {
		return err
	}
	defer src.Close()
	if err := unix.Renameat2(int(src.Fd()), receipt.Name, int(dst.Fd()), filepath.Base(relative), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return trash.Remove("info/" + receipt.Name + ".trashinfo")
}
