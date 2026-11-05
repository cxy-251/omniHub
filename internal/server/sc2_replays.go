package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"omnihub/internal/auth"
)

var sc2ReplayID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (r *Router) sc2ReplayDir(user int64) string {
	return filepath.Join(r.stateDir, "sc2-replays", strconv.FormatInt(user, 10))
}

type sc2Replay struct {
	ID        string        `json:"id"`
	CreatedAt int64         `json:"created_at"`
	Map       string        `json:"map"`
	Result    string        `json:"result"`
	Race      string        `json:"race"`
	Opponents []sc2Opponent `json:"opponents"`
	Mods      []string      `json:"mods"`
}

// Only complete app-owned bundles in this account's queue may be listed or played.
func (r *Router) sc2Replays(user int64) ([]sc2Replay, error) {
	root := r.sc2ReplayDir(user)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []sc2Replay{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows := []sc2Replay{}
	for _, e := range entries {
		if !e.IsDir() || !sc2ReplayID.MatchString(e.Name()) {
			continue
		}
		valid := true
		for _, name := range []string{"record.json", "game.SC2Replay", "map.SC2Map"} {
			info, err := os.Lstat(filepath.Join(root, e.Name(), name))
			if err != nil || !info.Mode().IsRegular() {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "record.json"))
		var row sc2Replay
		if err == nil && json.Unmarshal(b, &row) == nil && row.ID == e.Name() {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt == rows[j].CreatedAt {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].CreatedAt > rows[j].CreatedAt
	})
	return rows, nil
}

func sc2HostRequest(socket string, command any) (map[string]any, error) {
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return nil, err
	}
	if err = json.NewEncoder(conn).Encode(command); err != nil {
		return nil, err
	}
	var reply map[string]any
	if err = json.NewDecoder(conn).Decode(&reply); err != nil {
		return nil, err
	}
	if message, _ := reply["error"].(string); message != "" {
		return nil, errors.New(message)
	}
	return reply, nil
}

func (r *Router) handleSC2Replays(w http.ResponseWriter, req *http.Request) {
	if !r.gameControlAllowed(w, req) || !r.checkSectionAccess(w, req, "games", auth.TargetGUI) {
		return
	}
	user := auth.UserFromContext(req.Context())
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	rows, err := r.sc2Replays(user.ID)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	if req.Method == http.MethodGet {
		writeJSON(w, 200, rows)
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(req.Body).Decode(&in) != nil || !sc2ReplayID.MatchString(in.ID) {
		http.Error(w, "invalid replay", 400)
		return
	}
	found := false
	for _, row := range rows {
		if row.ID == in.ID {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "回放不存在或已被队列淘汰", 404)
		return
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		http.Error(w, "需要 uv 来播放回放", 503)
		return
	}
	dir, env, err := r.sc2Env()
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	r.sc2.mu.Lock()
	defer r.sc2.mu.Unlock()
	if r.gameRuns.HasRunning() {
		http.Error(w, "请先结束当前对局", 409)
		return
	}
	if _, err = r.ensureSC2Host(req.Context(), user.ID, uv, dir, env); err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	// A newly launched host needs a short interval to bind its private socket.
	var reply map[string]any
	for i := 0; i < 20; i++ {
		reply, err = sc2HostRequest(r.sc2.socket, map[string]string{"op": "replay", "id": in.ID})
		if err == nil {
			break
		}
		var netErr *net.OpError
		if !errors.As(err, &netErr) || netErr.Op != "dial" {
			break
		}
		select {
		case <-req.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, 200, reply)
}
