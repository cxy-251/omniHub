//go:build darwin

package platform

import (
	"golang.org/x/sys/unix"
	"runtime"
	"syscall"
	"unsafe"
)

func observeChildExit(pid int) error {
	// Darwin's waitid siginfo_t is 128 bytes; the exit observation needs no
	// field interpretation. WNOWAIT retains the child until exec.Cmd.Wait.
	var info [16]uint64
	for {
		_, _, err := syscall.Syscall6(unix.SYS_WAITID, 1, uintptr(pid), uintptr(unsafe.Pointer(&info[0])), unix.WEXITED|unix.WNOWAIT, 0, 0)
		runtime.KeepAlive(&info)
		if err == syscall.EINTR {
			continue
		}
		if err != 0 {
			return err
		}
		return nil
	}
}
