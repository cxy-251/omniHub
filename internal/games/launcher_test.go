package games

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeGameHelper(t *testing.T) {
	if os.Getenv("OMNIHUB_TEST_GAME_HELPER") != "1" {
		return
	}
	start := 0
	for i, arg := range os.Args {
		if arg == "--" {
			start = i + 1
			break
		}
	}
	time.Sleep(200 * time.Millisecond)
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"save": os.Getenv("OMNI_SAVE_DIR"), "args": os.Args[start:]}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestDetachedGameSurvivesRequestCancellationAndPreservesArgv(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "game")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	original, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "game-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	input, err := os.Open(original)
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
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"-test.run=^TestNativeGameHelper$", "--", "space separated", "$(not-a-shell-command)", "中文参数"}
	game, err := LaunchGame(ctx, LaunchOptions{GameDir: dir, ExePath: name, Args: args, Env: []string{"OMNIHUB_TEST_GAME_HELPER=1"}, UserID: 41, LogsDir: filepath.Join(root, "logs")})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := game.Wait(wait); err != nil {
		t.Fatalf("HTTP lifetime killed detached game: %v", err)
	}
	data, err := os.ReadFile(game.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Save string   `json:"save"`
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("helper log: %q %v", data, err)
	}
	if result.Save != filepath.Join(dir, "save", "users", "41") || strings.Join(result.Args, "|") != strings.Join(args[2:], "|") {
		t.Fatalf("argv/save boundary: %+v", result)
	}
	info, err := os.Stat(game.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("game logs not private")
	}
	if err := game.Wait(wait); err != nil {
		t.Fatal("completion cannot be observed twice")
	}
}

func TestGameSaveLinksAndEnvironmentOverridesRejected(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "save")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := PrepareSaveDir(dir, 41, false); err == nil {
		t.Fatal("save escaped through a link")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("external save directory modified")
	}
	if _, err := validateLaunch(LaunchOptions{GameDir: dir, ExePath: "game", LogsDir: dir, IsAdmin: true, Env: []string{"OMNI_SAVE_DIR=override"}}); err == nil {
		t.Fatal("save override accepted")
	}
	if _, err := validateLaunch(LaunchOptions{GameDir: dir, ExePath: "../game", LogsDir: dir, IsAdmin: true}); err == nil {
		t.Fatal("escaping executable accepted")
	}
	if _, err := validateLaunch(LaunchOptions{GameDir: dir, ExePath: "game", LogsDir: dir, IsAdmin: true, Args: []string{strings.Repeat("x", 4097)}}); err == nil {
		t.Fatal("oversized argument accepted")
	}
}

func TestGameLogCapsOutputWithoutBlockingProducer(t *testing.T) {
	var stored bytes.Buffer
	output := &gameLog{output: bufio.NewWriterSize(&stored, 16<<10), remaining: (4 << 20) - len(gameLogMarker) - gameLogTailBytes}
	data := bytes.Repeat([]byte{'x'}, (4<<20)+127)
	if n, err := output.Write(data); err != nil || n != len(data) {
		t.Fatal("log cap stalled the producer")
	}
	if n, err := output.Write([]byte("discarded")); err != nil || n != 9 {
		t.Fatal("subsequent output not drained")
	}
	if err := output.flush(); err != nil {
		t.Fatal(err)
	}
	if stored.Len() != 4<<20 {
		t.Fatalf("stored %d bytes", stored.Len())
	}
	if !strings.Contains(stored.String(), gameLogMarker) || !strings.HasSuffix(stored.String(), "discarded") {
		t.Fatal("log truncation marker or final crash context was lost")
	}
	if err := output.flush(); err != nil || stored.Len() != 4<<20 {
		t.Fatal("repeated flush duplicated the log tail")
	}
}
