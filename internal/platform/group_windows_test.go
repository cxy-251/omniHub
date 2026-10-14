package platform

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
)

func assertChildInactive(pid int) error {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	status, waitErr := windows.WaitForSingleObject(process, 0)
	closeErr := windows.CloseHandle(process)
	if status != windows.WAIT_OBJECT_0 {
		return errors.Join(fmt.Errorf("descendant %d remains active", pid), waitErr, closeErr)
	}
	return errors.Join(waitErr, closeErr)
}
