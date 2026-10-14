package platform

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOwnedGroupHelper(t *testing.T) {
	mode := os.Getenv("OMNIHUB_GROUP_HELPER")
	if mode == "" {
		return
	}
	if mode == "child" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if mode == "short" {
		time.Sleep(150 * time.Millisecond)
		os.Exit(0)
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	child := exec.Command(executable, "-test.run=^TestOwnedGroupHelper$")
	child.Env = append(os.Environ(), "OMNIHUB_GROUP_HELPER=child")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	fmt.Printf("CHILD_PID=%d\n", child.Process.Pid)
	if err := child.Process.Release(); err != nil {
		os.Exit(4)
	}
	time.Sleep(100 * time.Millisecond)
	os.Exit(0)
}

func TestZeroOwnedGroupCannotSignalCallerGroup(t *testing.T) {
	group := &ProcessGroup{}
	if !errors.Is(group.Stop(true), ErrInvalidProcessGroup) {
		t.Fatal("zero group permitted signal")
	}
	if !errors.Is(group.Wait(nil), ErrInvalidProcessGroup) {
		t.Fatal("zero group permitted wait")
	}
	if _, err := StartGroup(nil, false); !errors.Is(err, ErrInvalidProcessGroup) {
		t.Fatal("nil command permitted")
	}
}

func helperCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestOwnedGroupHelper$")
	cmd.Env = append(os.Environ(), "OMNIHUB_GROUP_HELPER="+mode)
	cmd.WaitDelay = time.Second
	return cmd
}

func TestOwnedGroupCleansInheritedOutputDescendant(t *testing.T) {
	cmd := helperCommand(t, "parent")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	group, err := StartGroup(cmd, true)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- group.Wait(nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		if err := group.Stop(true); err != nil {
			t.Error(err)
		}
		t.Fatal("descendant retained output or process identity")
	}
	text := strings.TrimSpace(output.String())
	pidText, ok := strings.CutPrefix(text, "CHILD_PID=")
	if !ok {
		t.Fatalf("child fixture failed: %q", text)
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatal(err)
	}
	if err := assertChildInactive(pid); err != nil {
		t.Fatal(err)
	}
	if err := group.Wait(nil); err != nil {
		t.Fatal("repeated wait changed completion")
	}
}

func TestRetiredGroupDoesNotSignalReplacementIdentity(t *testing.T) {
	first := helperCommand(t, "short")
	retired, err := StartGroup(first, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := retired.Wait(nil); err != nil {
		t.Fatal(err)
	}
	replacementCmd := helperCommand(t, "short")
	replacement, err := StartGroup(replacementCmd, false)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate numeric PID reuse without touching any unrelated system process.
	retired.pid = replacement.PID()
	if err := retired.Stop(false); err != nil {
		t.Fatal(err)
	}
	if err := retired.Stop(true); err != nil {
		t.Fatal(err)
	}
	if err := replacement.Wait(nil); err != nil {
		t.Fatalf("late stop killed replacement: %v", err)
	}
}
