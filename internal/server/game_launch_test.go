package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"omnihub/internal/auth"
	"omnihub/internal/games"
	"omnihub/internal/library"
	"omnihub/internal/scanner"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestIndexedNativeHelper(t *testing.T) {
	if os.Getenv("OMNIHUB_INDEXED_HELPER") != "1" {
		return
	}
	value := struct {
		Args      []string
		Env, Save string
	}{os.Args, os.Getenv("OMNIHUB_LITERAL_VALUE"), os.Getenv("OMNI_SAVE_DIR")}
	data, err := json.Marshal(value)
	if err == nil {
		err = os.WriteFile("observed.json", data, 0600)
	}
	if err != nil {
		os.Exit(2)
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func TestIndexedLaunchUsesOwningLibraryAndHostMetadata(t *testing.T) {
	state := t.TempDir()
	authDB, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authDB.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := authDB.CreateInitialAdmin("owner", "test-password"); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	root := t.TempDir()
	lib, err := manager.AddLibrary(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "standalone_games", "godot_games", "041 - Physical Folder")
	if err := os.Mkdir(folder, 0755); err != nil {
		t.Fatal(err)
	}
	name := "native-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := os.OpenFile(filepath.Join(folder, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(errors.Join(err, source.Close()))
	}
	_, copyErr := io.Copy(destination, source)
	if err := errors.Join(copyErr, source.Close(), destination.Close()); err != nil {
		t.Fatal(err)
	}
	expected := []string{"-test.run=^TestIndexedNativeHelper$", "--", "space and 中文", "", "$HOME", "a;b"}
	meta := map[string]any{"name": "Unrelated display title", "exe": name, "args": expected, "env": map[string]string{"OMNIHUB_INDEXED_HELPER": "1", "OMNIHUB_LITERAL_VALUE": "$HOME; literal 中文"}}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "omni.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanGames(filepath.Join(root, "standalone_games")); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(manager, authDB, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.gameStorageDirectory(root); err != nil {
		t.Fatalf("fixture index boundary: %v; libraries: %+v", err, manager.ListLibraries())
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	request := func(path, body, token string) *httptest.ResponseRecorder {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req := httptest.NewRequest("POST", path, bytes.NewBufferString(body)).WithContext(ctx)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	payload := `{"library_id":"` + lib.ID + `","id":"041"}`
	if w := request("/api/games/launch", payload, ""); w.Code != 403 {
		t.Fatalf("untrusted launch: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"library_id":"` + lib.ID + `","id":"041","exe":"arbitrary"}`, payload + ` {}`, `{"library_id":"missing","id":"041"}`} {
		if w := request("/api/games/launch", body, "native-token"); w.Code != 400 && w.Code != 404 {
			t.Fatalf("invalid identity accepted: %d %s", w.Code, w.Body.String())
		}
	}
	launched := request("/api/games/launch", payload, "native-token")
	var run games.RunStatus
	if err := json.Unmarshal(launched.Body.Bytes(), &run); err != nil || launched.Code != 201 || run.Key != lib.ID+":041" {
		t.Fatalf("launch: %d %s %v", launched.Code, launched.Body.String(), err)
	}
	if w := request("/api/games/launch", payload, "native-token"); w.Code != 409 {
		t.Fatalf("parallel launch: %d %s", w.Code, w.Body.String())
	}
	observedPath := filepath.Join(folder, "observed.json")
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err = os.ReadFile(observedPath)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			t.Fatalf("helper failed to run: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var observed struct {
		Args      []string
		Env, Save string
	}
	if err := json.Unmarshal(data, &observed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(observed.Args[1:], expected) || observed.Env != "$HOME; literal 中文" || observed.Save != filepath.Join(folder, "save") {
		t.Fatalf("host argv/save boundary changed: %+v", observed)
	}
	if w := request("/api/games/stop", `{"id":"`+run.ID+`"}`, "native-token"); w.Code != 202 {
		t.Fatalf("stop: %d %s", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	finished, err := router.gameRuns.Wait(ctx, run.ID)
	if err != nil || finished.State != "exited" {
		t.Fatalf("managed cleanup failed: %+v %v", finished, err)
	}
	manager.SetCategoryDisabled("standalone_games/godot_games", true)
	if w := request("/api/games/launch", payload, "native-token"); w.Code != 403 {
		t.Fatalf("disabled category launched: %d %s", w.Code, w.Body.String())
	}
}
