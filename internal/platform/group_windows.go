package platform

import (
	"errors"
	"golang.org/x/sys/windows"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

// ProcessGroup holds an actual process and Job Object, never a PID-map lookup.
// The process starts suspended and is assigned before any application thread runs.
type ProcessGroup struct {
	mu      sync.Mutex
	once    sync.Once
	cmd     *exec.Cmd
	pid     int
	job     windows.Handle
	process windows.Handle
	retired bool
	result  error
}

// StartGroup creates an owned Job/process identity; detached uses a new console group.
func StartGroup(cmd *exec.Cmd, detached bool) (*ProcessGroup, error) {
	if cmd == nil {
		return nil, ErrInvalidProcessGroup
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, errors.Join(err, windows.CloseHandle(job))
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.BELOW_NORMAL_PRIORITY_CLASS | windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		return nil, errors.Join(err, windows.CloseHandle(job))
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
	}
	if err == nil {
		err = resumeOwnedThreads(uint32(cmd.Process.Pid))
	}
	if err != nil {
		killErr := cmd.Process.Kill()
		waitErr := cmd.Wait()
		closeErr := windows.CloseHandle(job)
		if process != 0 {
			closeErr = errors.Join(closeErr, windows.CloseHandle(process))
		}
		return nil, errors.Join(err, killErr, waitErr, closeErr)
	}
	return &ProcessGroup{cmd: cmd, pid: cmd.Process.Pid, job: job, process: process}, nil
}

func resumeOwnedThreads(pid uint32) (result error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, windows.CloseHandle(snapshot)) }()
	entry := windows.ThreadEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	err = windows.Thread32First(snapshot, &entry)
	resumed := false
	for err == nil {
		if entry.OwnerProcessID == pid {
			thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if openErr != nil {
				return openErr
			}
			_, resumeErr := windows.ResumeThread(thread)
			if err := errors.Join(resumeErr, windows.CloseHandle(thread)); err != nil {
				return err
			}
			resumed = true
		}
		err = windows.Thread32Next(snapshot, &entry)
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	if !resumed {
		return errors.New("new process has no resumable thread")
	}
	return nil
}

// PID returns the original identifier for display/metrics, not identity lookups.
func (g *ProcessGroup) PID() int {
	if g == nil {
		return 0
	}
	return g.pid
}

// Stop signals the retained group or terminates its Job; retired groups do nothing.
func (g *ProcessGroup) Stop(force bool) error {
	if g == nil || g.cmd == nil || g.pid <= 0 || g.job == 0 || g.process == 0 {
		return ErrInvalidProcessGroup
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.retired {
		return nil
	}
	if force {
		return windows.TerminateJobObject(g.job, 1)
	}
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(g.pid)); err != nil {
		return windows.TerminateJobObject(g.job, 1)
	}
	return nil
}

// Wait retains process identity through exit and Job cleanup, drains output if
// requested, then closes owned handles. Only the first beforeReap hook runs.
func (g *ProcessGroup) Wait(beforeReap func() error) error {
	if g == nil || g.cmd == nil || g.pid <= 0 || g.job == 0 || g.process == 0 {
		return ErrInvalidProcessGroup
	}
	g.once.Do(func() {
		status, waitErr := windows.WaitForSingleObject(g.process, windows.INFINITE)
		if waitErr == nil && status != windows.WAIT_OBJECT_0 {
			waitErr = errors.New("unexpected process wait result")
		}
		g.mu.Lock()
		g.retired = true
		killErr := windows.TerminateJobObject(g.job, 1)
		g.mu.Unlock()
		var drainErr error
		if beforeReap != nil {
			drainErr = beforeReap()
		}
		g.result = errors.Join(waitErr, killErr, drainErr, g.cmd.Wait(), windows.CloseHandle(g.job), windows.CloseHandle(g.process))
	})
	return g.result
}
