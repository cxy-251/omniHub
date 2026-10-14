package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Optional real-browser check; ordinary Go tests never start a browser.
func checkChromeWebGame(t *testing.T, router *Router) {
	t.Helper()
	script := os.Getenv("OMNIHUB_WEB_GAME_BROWSER_CHECK")
	if script == "" {
		return
	}
	server := httptest.NewServer(router)
	defer server.Close()
	payload, err := json.Marshal(map[string]string{"url": server.URL, "cookie": "web-owner"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", script)
	command.Stdin = bytes.NewReader(payload)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real browser game check: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("WEB_GAME_CHECK_OK")) {
		t.Fatalf("browser did not confirm completion: %s", output)
	}
	t.Logf("real browser game check: %s", output)
}
