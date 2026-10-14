package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegisteredTaskLiteralFieldsAndPrivateMetadata(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	definitions := []Definition{{ID: "verify", Name: "Verify", Category: "media_library/manga", Executable: executable, Args: []string{"--fixed"}, Parameters: []Parameter{{Name: "input", Label: "Input", Required: true}}, Env: map[string]string{"SITE_TOKEN": "secret-value"}}}
	registry, err := NewRegistry(definitions, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(registry.List())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-value") || strings.Contains(string(data), executable) {
		t.Fatal("private command or credential exposed")
	}
	definitions[0].Env["SITE_TOKEN"] = "changed"
	definitions[0].Args[0] = "changed"
	command, err := registry.Prepare("verify", t.TempDir(), map[string]string{"input": "a b; $(touch bad)"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Args[0] != "--fixed" || command.Args[1] != "a b; $(touch bad)" || !strings.Contains(strings.Join(command.Environment, "|"), "SITE_TOKEN=secret-value") {
		t.Fatal("literal argument/immutable registration lost")
	}
	for _, values := range []map[string]string{{}, {"input": "ok", "unknown": "bad"}, {"input": strings.Repeat("x", 1025)}, {"input": "bad\x00value"}} {
		if _, err = registry.Prepare("verify", "", values); err == nil {
			t.Fatal("invalid form accepted")
		}
	}
	if _, err = registry.Prepare("unregistered", "", nil); err == nil {
		t.Fatal("unknown task accepted")
	}
	file := filepath.Join(t.TempDir(), "tasks.json")
	if err = os.WriteFile(file, []byte(`[] trailing`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadRegistry(filepath.Dir(file)); err == nil {
		t.Fatal("trailing registry data accepted")
	}
}

func TestTaskResourceOwnershipSurvivesDifferentIDs(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	defer manager.Close()
	root := t.TempDir()
	command := Command{Name: "wait", Executable: executable, Args: []string{"-test.run=^TestTaskLockHelper$"}, Environment: []string{"OMNIHUB_TASK_LOCK_HELPER=1"}, Resource: root, Write: true}
	first, err := manager.RunCommand("first", command)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, filepath.Join(root, "child"), filepath.Dir(root)} {
		command.Resource = path
		command.Write = false
		if _, err = manager.RunCommand("different", command); !errors.Is(err, ErrResourceBusy) {
			t.Fatalf("overlap %s: %v", path, err)
		}
	}
	// Sibling directories can run together; readers can share only after the
	// writer has actually exited and output/descendants have been collected.
	command.Resource = t.TempDir()
	second, err := manager.RunCommand("sibling", command)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.StopTask(first.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	select {
	case <-first.done:
	case <-ctx.Done():
		t.Fatal("writer did not exit")
	}
	command.Resource = root
	third, err := manager.RunCommand("reader", command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.RunCommand("second-reader", command); err != nil {
		t.Fatal(err)
	}
	if second == nil || third == nil {
		t.Fatal("missing actual child")
	}
	command.Write = true
	if _, err = manager.RunCommand("writer", command); !errors.Is(err, ErrResourceBusy) {
		t.Fatal("writer admitted over active readers")
	}
}

func TestTaskLockHelper(t *testing.T) {
	if os.Getenv("OMNIHUB_TASK_LOCK_HELPER") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}
