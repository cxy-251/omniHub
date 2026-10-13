package tasks

import (
	"context"
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
