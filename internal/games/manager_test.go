package games

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func managedFixture(t *testing.T) LaunchOptions {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "game")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "managed-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	if err := errors.Join(copyErr, input.Close(), output.Close()); err != nil {
		t.Fatal(err)
	}
	return LaunchOptions{GameDir: dir, ExePath: name, UserID: 41, LogsDir: filepath.Join(root, "logs"), Args: []string{"-test.run=^TestNativeGameHelper$", "--"}, Env: []string{"OMNIHUB_TEST_GAME_HELPER=1", "OMNIHUB_TEST_GAME_DELAY=10s"}}
}

func TestManagerOwnsOneGameAndStopsWithoutRequestLifetime(t *testing.T) {
	manager := NewManager()
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	opt := managedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	run, err := manager.Start(ctx, "library:001", opt)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if run.ID == "" || run.UserID != 41 || !manager.HasRunning() {
		t.Fatal("real session missing")
	}
	if _, err := manager.Start(context.Background(), "library:002", opt); !errors.Is(err, ErrRunBusy) {
		t.Fatalf("parallel native game: %v", err)
	}
	observer, stop := context.WithTimeout(context.Background(), time.Millisecond)
	defer stop()
	if _, err := manager.Wait(observer, run.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("observer cancellation: %v", err)
	}
	if !manager.HasRunning() {
		t.Fatal("cancelled observer stopped game")
	}
	snapshot, err := manager.Snapshot(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.State = "forged"
	if actual, err := manager.Snapshot(run.ID); err != nil || actual.State != "running" {
		t.Fatal("caller mutated session")
	}
	if err := manager.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	wait, release := context.WithTimeout(context.Background(), 7*time.Second)
	defer release()
	finished, err := manager.Wait(wait, run.ID)
	if err != nil || finished.State != "exited" || finished.EndedAt.IsZero() || manager.HasRunning() {
		t.Fatalf("completion: %+v %v", finished, err)
	}
	if err := manager.Stop(run.ID); err != nil {
		t.Fatal("late stop failed")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), "library:003", opt); !errors.Is(err, ErrRunClosed) {
		t.Fatalf("closed manager started game: %v", err)
	}
}

func TestManagerBoundsCompletedSessionHistory(t *testing.T) {
	manager := NewManager()
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	// History policy is independent of OS execution; real execution is covered above.
	manager.launch = func(context.Context, LaunchOptions) (*LaunchedGame, error) {
		done := make(chan struct{})
		close(done)
		return &LaunchedGame{PID: 1, StartTime: time.Now(), done: done}, nil
	}
	var first, last string
	for i := 0; i < 35; i++ {
		run, err := manager.Start(context.Background(), "library:001", LaunchOptions{UserID: 41})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = run.ID
		}
		last = run.ID
		if _, err := manager.Wait(context.Background(), run.ID); err != nil {
			t.Fatal(err)
		}
	}
	if first == last {
		t.Fatal("session identity reused")
	}
	if _, err := manager.Snapshot(first); !errors.Is(err, ErrRunUnknown) {
		t.Fatal("old history was not evicted")
	}
	if run, err := manager.Snapshot(last); err != nil || run.State != "exited" {
		t.Fatal("latest history missing")
	}
}

func TestManagerRecordsOnceBeforePublishingCompletion(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int
	recorderError := errors.New("state unavailable")
	manager := NewManagerWithRecorder(func(status RunStatus) error {
		calls++
		if status.State != "exited" || status.EndedAt.IsZero() {
			return errors.New("incomplete observation")
		}
		close(entered)
		<-release
		return recorderError
	})
	manager.launch = func(context.Context, LaunchOptions) (*LaunchedGame, error) {
		done := make(chan struct{})
		close(done)
		return &LaunchedGame{StartTime: time.Now(), done: done}, nil
	}
	run, err := manager.Start(context.Background(), "library:001", LaunchOptions{UserID: 41})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	active := manager.HasRunning()
	snapshot, err := manager.Snapshot(run.ID)
	close(release)
	if !active || err != nil || snapshot.State != "running" {
		t.Fatal("migration allowed before persistence completed", snapshot, err)
	}
	completed, err := manager.Wait(context.Background(), run.ID)
	if err != nil || completed.PersistenceError != recorderError.Error() || completed.ExitError != "" {
		t.Fatalf("persistence failure misreported: %+v %v", completed, err)
	}
	if _, err := manager.Wait(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("duplicate recording", calls)
	}
}
