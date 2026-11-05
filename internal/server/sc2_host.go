package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/games"
)

// Caller holds sc2.mu. The resident window has its own scope; each round's
// existing runtime session owns only its controller, so idle time is not played time.
func (r *Router) ensureSC2Host(ctx context.Context, userID int64, uv, dir string, env []string) (*games.LaunchedGame, error) {
	if r.sc2.host != nil && r.sc2.hostUser == userID {
		return r.sc2.host, nil
	}
	if err := r.closeSC2HostLocked(); err != nil {
		return nil, err
	}
	control, err := os.MkdirTemp(dir, "run-")
	if err != nil {
		return nil, err
	}
	socket := filepath.Join(control, "s")
	host, err := games.LaunchGame(ctx, games.LaunchOptions{
		GameDir: r.gameTools.SC2Dir, WorkDir: dir, Env: append(env, "SC2_REPLAY_DIR="+r.sc2ReplayDir(userID)), UserID: userID, IsAdmin: true,
		Argv:  []string{uv, "run", "--frozen", "--project", dir, "python", filepath.Join(dir, "resident.py"), "--socket", socket},
		Scope: fmt.Sprintf("app-omnihub-sc2-%d", time.Now().UnixNano()), LogsDir: filepath.Join(r.cacheDir, "game-logs"),
	})
	if err != nil {
		_ = os.Remove(control) // Empty, application-created control directory.
		return nil, err
	}
	r.sc2.host, r.sc2.hostUser, r.sc2.socket = host, userID, socket
	go func() {
		_ = host.Wait(context.Background())
		r.sc2.mu.Lock()
		defer r.sc2.mu.Unlock()
		if r.sc2.host == host {
			r.sc2.host, r.sc2.socket = nil, ""
			if active := r.gameRuns.Active(); active != nil && active.Key == "sc2:"+sc2GameID {
				_ = r.gameRuns.Stop(active.ID)
			}
		}
		_ = os.Remove(socket)
		_ = os.Remove(control)
	}()
	return host, nil
}

func sc2HostCommand(socket, command string) error {
	_, err := sc2HostRequest(socket, map[string]string{"op": command})
	return err
}

func (r *Router) closeSC2HostLocked() error {
	host := r.sc2.host
	if host == nil {
		return nil
	}
	if err := sc2HostCommand(r.sc2.socket, "close"); err != nil {
		if err := host.Stop(false); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := host.Wait(ctx); errors.Is(err, context.DeadlineExceeded) {
		if err := host.Stop(true); err != nil {
			return err
		}
		forced, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		if err := host.Wait(forced); errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
	r.sc2.host, r.sc2.socket = nil, ""
	return nil
}

func (r *Router) closeSC2Host() error {
	r.sc2.mu.Lock()
	defer r.sc2.mu.Unlock()
	return r.closeSC2HostLocked()
}

func (r *Router) handleSC2Control(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !r.gameControlAllowed(w, req) || !r.checkSectionAccess(w, req, "games", auth.TargetGUI) {
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if json.NewDecoder(req.Body).Decode(&in) != nil || (in.Action != "surrender" && in.Action != "close" && in.Action != "stop_replay") {
		http.Error(w, "invalid SC2 action", 400)
		return
	}
	r.sc2.mu.Lock()
	defer r.sc2.mu.Unlock()
	user := auth.UserFromContext(req.Context())
	if r.sc2.host != nil && r.sc2.hostUser != user.ID && user.Role != auth.RoleAdmin {
		http.Error(w, "SC2 window belongs to another account", 403)
		return
	}
	var err error
	if in.Action == "close" {
		err = r.closeSC2HostLocked()
	} else if r.sc2.host != nil {
		err = sc2HostCommand(r.sc2.socket, in.Action)
	}
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
