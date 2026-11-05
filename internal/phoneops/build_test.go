package phoneops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLocalBuildCancellationStopsChildren(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process ownership check")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runBuild(ctx, dir, "sh", []string{"-c", "sleep 30 & echo $! > " + Quote(pidFile) + "; wait"}, os.Environ(), func(string) {})
	}()
	var pid string
	for i := 0; i < 100; i++ {
		b, e := os.ReadFile(pidFile)
		if e == nil {
			pid = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == "" {
		t.Fatal("child did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("build children held job open")
	}
	b, e := os.ReadFile(filepath.Join("/proc", pid, "stat"))
	if e == nil && !strings.Contains(string(b), ") Z ") {
		t.Fatalf("build child still running: %s", b)
	}
}
func TestLocalBuildRejectsInvalidInputs(t *testing.T) {
	dir := t.TempDir()
	if _, err := ProjectRoot(dir); err == nil {
		t.Fatal("non-project accepted")
	}
	file := filepath.Join(dir, "server")
	if err := os.WriteFile(file, []byte("not a phone executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if ValidateServer(file) == nil {
		t.Fatal("invalid server accepted")
	}
}
