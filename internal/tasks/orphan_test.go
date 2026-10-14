package tasks

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestTaskOutputDescendantHelper(t *testing.T) {
	mode := os.Getenv("OMNIHUB_TASK_DESCENDANT")
	if mode == "" {
		return
	}
	if mode == "child" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	child := exec.Command(executable, "-test.run=^TestTaskOutputDescendantHelper$")
	child.Env = append(os.Environ(), "OMNIHUB_TASK_DESCENDANT=child")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	if err := child.Process.Release(); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func TestTaskCompletesWhenDescendantKeepsOutputOpen(t *testing.T) {
	t.Setenv("OMNIHUB_TASK_DESCENDANT", "parent")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	task, err := manager.RunScript("descendant", executable, []string{"-test.run=^TestTaskOutputDescendantHelper$"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-task.done:
	case <-time.After(5 * time.Second):
		t.Fatal("task waited forever on descendant stdout")
	}
	if task.isRunning() {
		t.Fatal("completed group remained active")
	}
}
