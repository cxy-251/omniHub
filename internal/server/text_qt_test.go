package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/library"
)

func TestNativeTextAcrossQtProcesses(t *testing.T) {
	checker := os.Getenv("OMNIHUB_QT_TEXT_CHECK")
	if checker == "" {
		t.Skip("optional real Qt text reader check")
	}
	state, root, config := t.TempDir(), t.TempDir(), t.TempDir()
	users, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer users.Close()
	if _, err = users.CreateInitialAdmin("owner", "test-password"); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	if _, err = manager.AddLibrary(root, false); err != nil {
		t.Fatal(err)
	}
	book := "\n\n" + strings.Repeat("a", 16381) + "😀end"
	for i := 3; i < 82; i++ {
		book += fmt.Sprintf("\nline%d", i)
	}
	for i := 0; i < 26; i++ {
		content := "placeholder"
		if i == 0 {
			content = book
		}
		if err = os.WriteFile(filepath.Join(root, "media_library", "docs", fmt.Sprintf("%02d.txt", i)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// All account writes must go to global state, never into the read-only library.
	manager = library.NewManager()
	if _, err = manager.MountLibrary(root, false, true); err != nil {
		t.Fatal(err)
	}
	if err = manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(manager, users, state, "text-token")
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	server := httptest.NewServer(router)
	defer server.Close()
	connection, err := json.Marshal(map[string]string{"url": server.URL, "token": "text-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(config, "local-client.json"), connection, 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"save", "restore"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, checker, mode)
		cmd.Env = append(os.Environ(), "OMNIHUB_CONFIG_DIR="+config)
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("Qt text %s: %v\n%s", mode, err, output)
		}
		t.Log(string(output))
	}
}
