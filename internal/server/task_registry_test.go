package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/tasks"
)

func TestRegisteredTaskAPIRejectsPathsAndProtectsLibrary(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := tasks.NewRegistry([]tasks.Definition{{ID: "write", Name: "Writer", Category: "media_library/docs", Write: true, Executable: executable, Args: []string{"-test.run=^TestRegisteredTaskAPIHelper$"}, Parameters: []tasks.Parameter{{Name: "value", Label: "Value", Required: true}}, Env: map[string]string{"OMNIHUB_TASK_API_HELPER": "1", "SITE_TOKEN": "private-credential"}}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state, root := t.TempDir(), t.TempDir()
	users, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer users.Close()
	if _, err = users.CreateInitialAdmin("owner", "test-password"); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	lib, err := manager.AddLibrary(root, false)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(manager, users, state, "token", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	catalog := request("GET", "/api/tasks/catalog", "")
	if catalog.Code != 200 || strings.Contains(catalog.Body.String(), "private-credential") || strings.Contains(catalog.Body.String(), executable) {
		t.Fatal("unsafe task catalog")
	}
	body := func(dir string) string {
		data, _ := json.Marshal(map[string]any{"definition": "write", "library_id": lib.ID, "directory": dir, "parameters": map[string]string{"value": "space ; $(literal)"}})
		return string(data)
	}
	for _, invalid := range []string{`{"script":"/bin/sh","args":["-c","bad"]}`, `{"definition":"unknown"}`, body("../../"), body("/absolute")} {
		if w := request("POST", "/api/tasks/run", invalid); w.Code != 400 {
			t.Fatalf("unsafe task admitted: %d %s", w.Code, w.Body.String())
		}
	}
	w := request("POST", "/api/tasks/run", body(""))
	if w.Code != 200 {
		t.Fatalf("run %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "test.run") || strings.Contains(w.Body.String(), "args") {
		t.Fatal("private task arguments exposed in start response")
	}
	var task struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &task); err != nil || task.ID == "" {
		t.Fatal("missing task identity")
	}
	live := router.taskMgr.GetTask(task.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	channel, history := live.SubscribeLogs(ctx)
	for entry := range channel {
		history = append(history, entry)
	}
	data, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "literal field accepted") || !strings.Contains(string(data), "correct working directory") {
		t.Fatalf("actual command failed: %s", data)
	}
	stream := request("GET", "/api/tasks/logs?id="+task.ID, "")
	if stream.Code != 200 || !strings.Contains(stream.Body.String(), "event: end") || !strings.Contains(stream.Body.String(), "id: ") {
		t.Fatal("actual SSE history/end missing")
	}
	if w = request("POST", "/api/tasks/stop", `{"id":"`+task.ID+`"}`); w.Code != 200 {
		t.Fatal("completed stop is not idempotent")
	}
	// Re-mount the exact same resource read-only; registered writes are denied.
	manager.RemoveLibrary(lib.ID)
	if _, err = manager.MountLibrary(root, false, true); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", "/api/tasks/run", body("")); w.Code != 403 {
		t.Fatalf("read-only writer admitted: %d %s", w.Code, w.Body.String())
	}
}

func TestRegisteredTaskAPIHelper(t *testing.T) {
	if os.Getenv("OMNIHUB_TASK_API_HELPER") != "1" {
		return
	}
	if os.Args[len(os.Args)-1] != "space ; $(literal)" {
		os.Exit(2)
	}
	cwd, err := os.Getwd()
	if err != nil || cwd != os.Getenv("OMNIHUB_TASK_DIR") || os.Getenv("SITE_TOKEN") != "private-credential" {
		os.Exit(3)
	}
	println("INFO literal field accepted")
	println("INFO correct working directory")
	os.Exit(0)
}
