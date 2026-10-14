package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"omnihub/internal/games"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// checkQtGameController is opt-in: regular Go tests require no Qt installation.
// The caller fixture provides a real copied game executable and metadata.
func checkQtGameController(t *testing.T, router *Router, libraryID, root, observed string) {
	t.Helper()
	checker := os.Getenv("OMNIHUB_QT_GAME_CHECK")
	if checker == "" {
		return
	}
	db, err := games.OpenDB(filepath.Join(root, "standalone_games"))
	if err != nil {
		t.Fatal(err)
	}
	for id := 42; id <= 66; id++ {
		if err := db.UpsertGame(&games.Game{ID: fmt.Sprintf("%03d", id), Name: "Other game", Category: "godot", Target: "gui_only"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(observed); err != nil {
		t.Fatal(err)
	} // Owned helper output, not a user asset.
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
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, checker, libraryID, observed)
	command.Env = append(os.Environ(), "OMNIHUB_CONFIG_DIR="+config)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual Qt game controller: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("GAME_CHECK_OK:")) {
		t.Fatalf("Qt checker did not confirm completion: %s", output)
	}
	t.Logf("actual Qt game controller: %s", output)
	if _, err := os.Stat(observed); err != nil {
		t.Fatal("Qt launch did not execute the actual helper")
	}
	if router.gameRuns.HasRunning() {
		t.Fatal("Qt stop left active game")
	}
}
