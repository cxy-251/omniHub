package server

import (
	"net/http"
	"net/http/httptest"
	"omnihub/internal/auth"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSC2ReplayAccountAndPathBoundary(t *testing.T) {
	root := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(root, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.CreateUser("admin", "password", auth.RoleAdmin, auth.LevelAdmin); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateSession(1, "web-session", time.Hour); err != nil {
		t.Fatal(err)
	}
	r := &Router{authDB: db, stateDir: root}
	id := strings.Repeat("a", 32)
	folder := filepath.Join(r.sc2ReplayDir(1), id)
	if err = os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"record.json": `{"id":"` + id + `","created_at":1}`, "game.SC2Replay": "replay", "map.SC2Map": "map"} {
		if err = os.WriteFile(filepath.Join(folder, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if rows, err := r.sc2Replays(1); err != nil || len(rows) != 1 {
		t.Fatalf("own queue: %v %v", rows, err)
	}
	if rows, err := r.sc2Replays(2); err != nil || len(rows) != 0 {
		t.Fatalf("other account: %v %v", rows, err)
	}
	handler := auth.NewMiddleware(db, "local-secret").Handler(http.HandlerFunc(r.handleSC2Replays))
	for _, tc := range []struct {
		local bool
		body  string
		want  int
	}{{false, `{"id":"` + id + `"}`, 403}, {true, `{"id":"../outside"}`, 400}, {true, `{"id":"` + strings.Repeat("b", 32) + `"}`, 404}} {
		req := httptest.NewRequest("POST", "/api/sc2/replays", strings.NewReader(tc.body))
		req.RemoteAddr = "127.0.0.1:1234"
		if tc.local {
			req.Header.Set("X-Omni-Local-Token", "local-secret")
		} else {
			req.AddCookie(&http.Cookie{Name: "omni_token", Value: "web-session"})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("status %d want %d: %s", w.Code, tc.want, w.Body)
		}
	}
	if err = os.Remove(filepath.Join(folder, "game.SC2Replay")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(root, "users.db"), filepath.Join(folder, "game.SC2Replay")); err != nil {
		t.Fatal(err)
	}
	if rows, err := r.sc2Replays(1); err != nil || len(rows) != 0 {
		t.Fatalf("symlink accepted: %v %v", rows, err)
	}
}
