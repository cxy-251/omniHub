package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/storage"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeResumeInSeparateQtProcesses(t *testing.T) {
	checker, media := os.Getenv("OMNIHUB_QT_RESUME_CHECK"), os.Getenv("OMNIHUB_QT_RESUME_MEDIA")
	if checker == "" {
		t.Skip("optional real Qt/native resume check")
	}
	if media == "" {
		t.Fatal("OMNIHUB_QT_RESUME_MEDIA required")
	}
	state := t.TempDir()
	users, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer users.Close()
	owner, err := users.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(library.NewManager(), users, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
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
	for _, mode := range []string{"save", "restore"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, checker, media, mode)
		cmd.Env = append(os.Environ(), "OMNIHUB_CONFIG_DIR="+config)
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("actual %s: %v\n%s", mode, err, output)
		}
		t.Log(string(output))
	}
	db, err := storage.OpenSQLite(filepath.Join(state, "user_state.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var account int64
	var position, volume, rate float64
	if err := db.QueryRow("SELECT user_id,position,volume,rate FROM native_progress").Scan(&account, &position, &volume, &rate); err != nil || account != owner.ID || position != 4.3 || volume != 42 || rate != 1.5 {
		t.Fatalf("global account state mismatch: %d %f %f %f %v", account, position, volume, rate, err)
	}
}
