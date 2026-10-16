package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	sc2assets "omnihub/features/sc2"
	"omnihub/internal/auth"
	"omnihub/internal/games"
	"omnihub/internal/platform"
)

// 星际争霸 2（规格 10.1）：在游戏大厅 Wine 分类里显示为一张卡片，选中后是对战面板。
// 地图解析与对局由 features/sc2 的 Python 助手完成（uv 子进程）；对局作为一个游戏会话运行在独立 scope。
const sc2GameID = "wine/StarCraft II"

type sc2State struct {
	mu        sync.Mutex
	lastRunID string
	mods      []byte
}

func (r *Router) sc2Available() bool {
	info, err := os.Stat(filepath.Join(r.gameTools.SC2Dir, "Maps"))
	return r.gameTools.SC2Dir != "" && err == nil && info.IsDir()
}

// sc2Env 返回运行助手所需的目录与环境变量。
func (r *Router) sc2Env() (dir string, env []string, err error) {
	dir, err = sc2assets.Prepare(filepath.Join(r.cacheDir, "sc2"))
	if err != nil {
		return "", nil, err
	}
	env = []string{
		"SC2PATH=" + r.gameTools.SC2Dir,
		"SC2_THUMB_CACHE=" + filepath.Join(dir, "thumbs"),
		"SC2MOD_DIR=" + r.gameTools.SC2ModDir,
		"SC2MOD_COMPAT_DATA=" + r.gameTools.ProtonPrefix,
		"WINE=" + filepath.Join(dir, "proton-wine"),
		"OMNIHUB_STEAM_ROOT=" + r.gameTools.SteamRoot,
		"UV_PROJECT_ENVIRONMENT=" + filepath.Join(dir, "venv"),
	}
	return dir, env, nil
}

// sc2Helper 以低优先级运行助手的查询子命令（maps/mods），返回标准输出。
func (r *Router) sc2Helper(ctx context.Context, sub string) ([]byte, error) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		return nil, errors.New("需要 uv 来运行 SC2 助手")
	}
	dir, env, err := r.sc2Env()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := platform.BackgroundCommand(ctx, uv, "run", "--frozen", "--project", dir, "python", filepath.Join(dir, "sc2tool.py"), sub)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("SC2 助手失败：%v %s", err, strings.TrimSpace(lastLines(stderr.String(), 3)))
	}
	return out, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// handleSC2Maps 返回地图清单；结果缓存在配置目录，地图目录有变化才重新解析（首次约 10 秒）。
func (r *Router) handleSC2Maps(w http.ResponseWriter, req *http.Request) {
	if !r.sc2Available() {
		http.Error(w, "StarCraft II not found", 404)
		return
	}
	cache := filepath.Join(r.cacheDir, "sc2", "maps.json")
	mapsInfo, _ := os.Stat(filepath.Join(r.gameTools.SC2Dir, "Maps"))
	if info, err := os.Stat(cache); err == nil && mapsInfo != nil && !info.ModTime().Before(mapsInfo.ModTime()) {
		if data, err := os.ReadFile(cache); err == nil {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}
	}
	data, err := r.sc2Helper(req.Context(), "maps")
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	_ = os.WriteFile(cache, data, 0644)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// 地图名（不含扩展名）：可含非 ASCII 字符（如 Außenposten），但不能含路径分隔符、控制字符或 ".."。
var mapStemPattern = regexp.MustCompile(`^[^/\\\x00-\x1f]{1,128}$`)

// handleSC2Thumb 返回地图缩略图（maps 子命令生成的 300×300 PNG）。
func (r *Router) handleSC2Thumb(w http.ResponseWriter, req *http.Request) {
	stem := req.URL.Query().Get("stem")
	if !mapStemPattern.MatchString(stem) || strings.Contains(stem, "..") {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, req, filepath.Join(r.cacheDir, "sc2", "thumbs", stem+".png"))
}

// handleSC2Mods 返回可选 mod（来自 sc2Mod 项目，没有时为空）。
func (r *Router) handleSC2Mods(w http.ResponseWriter, req *http.Request) {
	r.sc2.mu.Lock()
	cached := r.sc2.mods
	r.sc2.mu.Unlock()
	if cached == nil {
		data, err := r.sc2Helper(req.Context(), "mods")
		if err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		r.sc2.mu.Lock()
		r.sc2.mods, cached = data, data
		r.sc2.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(cached)
}

type sc2Opponent struct {
	Race       string `json:"race"`
	Difficulty string `json:"difficulty"`
	AIBuild    string `json:"ai_build"`
}

var (
	sc2Races   = map[string]bool{"T": true, "P": true, "Z": true, "R": true}
	sc2Diffs   = map[string]bool{"veryeasy": true, "easy": true, "medium": true, "mediumhard": true, "hard": true, "harder": true, "veryhard": true, "cheatvision": true, "cheatmoney": true, "cheatinsane": true}
	sc2Builds  = map[string]bool{"": true, "random": true, "rush": true, "timing": true, "power": true, "macro": true, "air": true}
	modPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
)

// handleSC2Play 开一局：你一个真人 + 若干电脑。对局是一个游戏会话（独立 scope，同时只能有一个游戏）。
func (r *Router) handleSC2Play(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !r.gameControlAllowed(w, req) || !r.checkSectionAccess(w, req, "games", auth.TargetGUI) {
		return
	}
	var in struct {
		Map       string        `json:"map"`
		Race      string        `json:"race"`
		Opponents []sc2Opponent `json:"opponents"`
		Mods      []string      `json:"mods"`
	}
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil || !mapStemPattern.MatchString(in.Map) || !sc2Races[in.Race] || len(in.Opponents) < 1 || len(in.Opponents) > 7 || len(in.Mods) > 8 {
		http.Error(w, "invalid match settings", 400)
		return
	}
	for _, o := range in.Opponents {
		if !sc2Races[o.Race] || !sc2Diffs[o.Difficulty] || !sc2Builds[o.AIBuild] {
			http.Error(w, "invalid opponent", 400)
			return
		}
	}
	for _, m := range in.Mods {
		if !modPattern.MatchString(m) {
			http.Error(w, "invalid mod", 400)
			return
		}
	}
	// 同一 group 的 mod 互斥（如 3 倍 / 5 倍采集、2 倍运营 / 秒建造），定义来自 sc2Mod 的 MOD_INFO。
	r.sc2.mu.Lock()
	cached := r.sc2.mods
	r.sc2.mu.Unlock()
	var infos []struct {
		Key   string `json:"key"`
		Name  string `json:"name"`
		Group string `json:"group"`
	}
	if cached != nil && json.Unmarshal(cached, &infos) == nil {
		chosen := map[string]string{}
		for _, m := range in.Mods {
			for _, info := range infos {
				if info.Key != m || info.Group == "" {
					continue
				}
				if other, dup := chosen[info.Group]; dup {
					http.Error(w, other+" 与 "+info.Name+" 不能同时选", 400)
					return
				}
				chosen[info.Group] = info.Name
			}
		}
	}
	if !r.sc2Available() {
		http.Error(w, "StarCraft II not found", 404)
		return
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		http.Error(w, "需要 uv 来运行 SC2 助手", 503)
		return
	}
	dir, env, err := r.sc2Env()
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	opponents, _ := json.Marshal(in.Opponents)
	argv := []string{uv, "run", "--frozen", "--project", dir, "python", filepath.Join(dir, "sc2tool.py"), "play",
		"--map", in.Map, "--race", in.Race, "--opponents", string(opponents), "--mods", strings.Join(in.Mods, ",")}
	user := auth.UserFromContext(req.Context())
	status, err := r.gameRuns.Start(req.Context(), "sc2:"+sc2GameID, games.LaunchOptions{
		GameDir: r.gameTools.SC2Dir, WorkDir: dir, Argv: argv, Env: env,
		Scope: fmt.Sprintf("app-omnihub-game-%d", time.Now().UnixNano()), UserID: user.ID, IsAdmin: true,
		LogsDir: filepath.Join(r.cacheDir, "game-logs"),
	})
	if errors.Is(err, games.ErrRunBusy) {
		http.Error(w, "a game is already running", 409)
		return
	}
	if err != nil {
		http.Error(w, "launch failed: "+err.Error(), 422)
		return
	}
	r.sc2.mu.Lock()
	r.sc2.lastRunID = status.ID
	r.sc2.mu.Unlock()
	writeJSON(w, http.StatusCreated, status)
}

// handleSC2Status 返回是否可用、当前/上一局会话与结果（从对局日志末尾的 OMNIHUB_SC2_RESULT 行读取）。
func (r *Router) handleSC2Status(w http.ResponseWriter, req *http.Request) {
	out := map[string]any{"available": r.sc2Available(), "mods_available": r.gameTools.SC2ModDir != ""}
	r.sc2.mu.Lock()
	id := r.sc2.lastRunID
	r.sc2.mu.Unlock()
	if id != "" {
		if status, err := r.gameRuns.Snapshot(id); err == nil {
			out["session"] = status
			// 结果一写进日志就报告（SC2 进程可能还在收尾），界面据此立刻显示「对局已结束」。
			if result := sc2ResultFromLog(status.LogPath, status.State == "exited"); result != nil {
				out["result"] = result
				out["ended"] = true
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// sc2ResultFromLog 读日志里最后一行结果；还没有结果时，进程已退出则报异常结束，否则返回 nil（仍在对局）。
func sc2ResultFromLog(path string, exited bool) any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var last string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		if line := scanner.Text(); strings.HasPrefix(line, "OMNIHUB_SC2_RESULT ") {
			last = strings.TrimPrefix(line, "OMNIHUB_SC2_RESULT ")
		}
	}
	var result any
	if last == "" || json.Unmarshal([]byte(last), &result) != nil {
		if !exited {
			return nil
		}
		return map[string]string{"result": "Error", "error": "对局异常结束，详见日志"}
	}
	return result
}
