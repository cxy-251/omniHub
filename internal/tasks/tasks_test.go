package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskManagerLifecycleAndStreaming(t *testing.T) {
	tmpDir := t.TempDir()
	scriptFile := filepath.Join(tmpDir, "test_job.sh")
	scriptContent := `#!/bin/sh
echo "INFO: Starting job"
echo "WARN: Slow network"
echo "ERROR: Failed to connect"
echo "    at main.go:10"
sleep 1
echo "INFO: Job finished"
`
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("write script error: %v", err)
	}

	mgr := NewManager()
	task, err := mgr.RunScript("task-1", scriptFile, nil)
	if err != nil {
		t.Fatalf("RunScript failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	logCh, _ := task.SubscribeLogs(ctx)
	received := 0
	hasError := false
	hasStack := false

	for entry := range logCh {
		received++
		if entry.Level == LevelError {
			hasError = true
		}
		if entry.IsStack {
			hasStack = true
		}
	}

	if received < 4 {
		t.Errorf("expected at least 4 log entries, got %d", received)
	}
	if !hasError {
		t.Errorf("expected to detect ERROR log level")
	}
	if !hasStack {
		t.Errorf("expected to detect stack trace")
	}
}

func TestCompletedSubscriptionReturnsOrderedBoundedHistory(t *testing.T) {
	task := &Task{listeners: make(map[chan LogEntry]struct{})}
	for i := 0; i < MaxLogEntries+17; i++ {
		task.appendLog("INFO: retained output")
	}
	ch, history := task.SubscribeLogs(context.Background())
	if len(history) != MaxLogEntries || history[0].ID != 18 {
		t.Fatalf("invalid retained history: %d, %d", len(history), history[0].ID)
	}
	for i := 1; i < len(history); i++ {
		if history[i].ID != history[i-1].ID+1 {
			t.Fatal("history out of order")
		}
	}
	if _, open := <-ch; open {
		t.Fatal("completed task left subscription open")
	}
}

func TestCancelSubscriptionAndShutdown(t *testing.T) {
	script := filepath.Join(t.TempDir(), "wait.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho started\nsleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager()
	task, err := mgr.RunScript("wait", script, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := task.SubscribeLogs(ctx)
	cancel()
	timer := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				goto closed
			}
		case <-timer:
			t.Fatal("subscription was not closed")
		}
	}
closed:
	// HTTP 状态快照与退出并发执行。
	jsonDone := make(chan struct{})
	go func() {
		defer close(jsonDone)
		for i := 0; i < 100; i++ {
			_, _ = json.Marshal(task)
		}
	}()
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
	<-jsonDone
	if task.isRunning() {
		t.Fatal("task still running after close")
	}
	if _, err := mgr.RunScript("again", script, nil); err == nil {
		t.Fatal("closed manager accepted task")
	}
}
