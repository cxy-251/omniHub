package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt-in native handoff: original GUI must exit while the actual game survives;
// the detached Core observer restores state only after service-owned completion.
func checkQtGameYield(t *testing.T, router *Router, session string) {
	t.Helper()
	checker := os.Getenv("OMNIHUB_QT_YIELD_CHECK")
	if checker == "" {
		return
	}
	server := httptest.NewServer(router)
	defer server.Close()
	config := t.TempDir()
	data, err := json.Marshal(map[string]string{"url": server.URL, "token": "native-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "local-client.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, checker, session)
	command.Env = append(os.Environ(), "OMNIHUB_CONFIG_DIR="+config)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("GUI yield: %v\n%s", err, output)
	}
	if !router.gameRuns.HasRunning() {
		t.Fatal("GUI teardown terminated the managed game")
	}
	data, err = os.ReadFile(filepath.Join(config, "yield-observer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var process struct {
		ObserverPID int `json:"observer_pid"`
		GuiPID      int `json:"gui_pid"`
	}
	if err := json.Unmarshal(data, &process); err != nil || process.ObserverPID <= 0 || process.GuiPID != command.Process.Pid {
		t.Fatalf("observer identity: %s %v", data, err)
	}
	// Diagnostic only: never signal an old numeric process identity.
	if status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(process.ObserverPID), "smaps_rollup")); err == nil {
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "Rss:") || strings.HasPrefix(line, "Pss:") || strings.HasPrefix(line, "Private_Dirty:") {
				t.Logf("CORE_OBSERVER %s", line)
			}
		}
	}
	if err := router.gameRuns.Stop(session); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		data, err = os.ReadFile(filepath.Join(config, "yield-restored.json"))
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			t.Fatalf("restored GUI missing: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var result struct {
		Restored bool `json:"restored"`
	}
	if err := json.Unmarshal(data, &result); err != nil || !result.Restored {
		t.Fatalf("UI snapshot not restored: %s %v", data, err)
	}
	t.Log("GUI_YIELD_OK: original process exited, real game survived, Core observer restored snapshot after exit")
}
