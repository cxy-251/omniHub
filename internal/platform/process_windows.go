package platform

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var jobs = struct {
	sync.Mutex
	handles map[int]windows.Handle
}{handles: make(map[int]windows.Handle)}

// BackgroundCommand uses Windows below-normal process priority instead of Unix utilities.
func BackgroundCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, executable, args...)
}

// Start creates a Windows process group and tracks descendants in a kill-on-close Job Object.
func Start(cmd *exec.Cmd, detached bool) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return errors.Join(err, windows.CloseHandle(job))
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.BELOW_NORMAL_PRIORITY_CLASS}
	if err := cmd.Start(); err != nil {
		return errors.Join(err, windows.CloseHandle(job))
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		err = errors.Join(err, windows.CloseHandle(process))
	}
	if err != nil {
		killErr := cmd.Process.Kill()
		waitErr := cmd.Wait()
		return errors.Join(err, killErr, waitErr, windows.CloseHandle(job))
	}
	jobs.Lock()
	jobs.handles[cmd.Process.Pid] = job
	jobs.Unlock()
	return nil
}

// Stop requests console shutdown, or forcibly terminates every process in the tracked Job Object.
func Stop(pid int, force bool) error {
	if pid <= 0 {
		return errors.New("invalid process group ID")
	}
	jobs.Lock()
	defer jobs.Unlock()
	job, ok := jobs.handles[pid]
	if !ok {
		return nil
	}
	if force {
		return windows.TerminateJobObject(job, 1)
	}
	// GUI processes may not handle CTRL_BREAK; the caller's timeout escalates to Job termination.
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid)); err != nil {
		return windows.TerminateJobObject(job, 1)
	}
	return nil
}

// Release closes the tracked Job Object, cleaning up descendants without targeting reused PIDs.
func Release(pid int) error {
	jobs.Lock()
	defer jobs.Unlock()
	job, ok := jobs.handles[pid]
	if !ok {
		return nil
	}
	delete(jobs.handles, pid)
	return windows.CloseHandle(job)
}
