package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/games"
	"omnihub/internal/library"
)

// 网页游戏分类（只在 Web 端展示与运行，规格 4.2）。
var webGameCategories = []string{"rpg", "slg", "retro"}

// ConfigureGames 设置非原生游戏的运行时位置（Proton、Wine 容器、Lime3DS、Ren'Py SDK）。
func (r *Router) ConfigureGames(tools games.Tools) { r.gameTools = tools }

// gameCategoriesFor 按客户端端目标返回可见分类：本地客户端只看原生游戏，Web 只看网页游戏。
func gameCategoriesFor(req *http.Request) []string {
	if auth.ClientFromContext(req.Context()) == auth.TargetGUI {
		return games.NativeCategories
	}
	return webGameCategories
}

func (r *Router) categoryDir(libID, category string) (string, error) {
	rel := "standalone_games/" + category + "_games"
	if r.libMgr.IsCategoryDisabled(rel) {
		return "", errors.New("category disabled")
	}
	return r.libMgr.ResolveCategoryPath(libID+":"+rel, rel)
}

type gameEntry struct {
	games.Entry
	PlayTimeSec  int64     `json:"play_time_sec,omitempty"`
	LastPlayedAt time.Time `json:"last_played_at,omitzero"`
}

// handleGameList 列出当前端可见的全部游戏（各库合并，同一分类同名目录只取第一个库）。
// category 为空时返回全部；数量只有几百，不分页。
func (r *Router) handleGameList(w http.ResponseWriter, req *http.Request) {
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	want := req.URL.Query().Get("category")
	user := auth.UserFromContext(req.Context())
	seen := map[string]bool{}
	out := []gameEntry{}
	for _, category := range gameCategoriesFor(req) {
		if want != "" && want != category {
			continue
		}
		for _, lib := range libs {
			dir, err := r.categoryDir(lib.ID, category)
			if err != nil {
				continue
			}
			list, err := games.ScanCategory(dir, category, lib.ID)
			if err != nil {
				http.Error(w, "game directory unavailable", http.StatusServiceUnavailable)
				return
			}
			for _, e := range list {
				if seen[e.ID] {
					continue
				}
				seen[e.ID] = true
				out = append(out, gameEntry{Entry: e})
			}
		}
	}
	// 账号游玩时长（按 100 个一批查询）。
	if user.ID > 0 && r.userState != nil {
		for start := 0; start < len(out); start += 100 {
			batch := out[start:min(start+100, len(out))]
			ids := make([]string, len(batch))
			for i, g := range batch {
				ids[i] = g.ID
			}
			stats, err := r.userState.GameStats(req.Context(), user.ID, ids)
			if err != nil {
				break
			}
			for i := range batch {
				if s, ok := stats[batch[i].ID]; ok {
					batch[i].PlayTimeSec, batch[i].LastPlayedAt = s.PlayTimeSec, s.LastPlayedAt
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (r *Router) findLibrary(id string) *library.Library {
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.ID == id {
			return lib
		}
	}
	return nil
}

// gameDir 解析 library_id + 游戏 ID（分类/目录名）到真实目录。
func (r *Router) gameDir(libID, id string) (category, dir string, err error) {
	category, folder, ok := strings.Cut(id, "/")
	if !ok || folder == "" || strings.ContainsAny(folder, "/\\\x00") || strings.HasPrefix(folder, ".") {
		return "", "", errors.New("invalid game id")
	}
	known := false
	for _, c := range append(append([]string{}, games.NativeCategories...), webGameCategories...) {
		known = known || c == category
	}
	if !known {
		return "", "", errors.New("unknown game category")
	}
	rel := "standalone_games/" + category + "_games"
	dir, err = r.libMgr.ResolveCategoryPath(libID+":"+rel+"/"+folder, rel)
	if err != nil {
		return "", "", err
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return "", "", errors.New("game directory not found")
	}
	return category, dir, nil
}

// handleGameIcon 返回游戏图标（omni.json icon 或常见文件名）。
func (r *Router) handleGameIcon(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	_, dir, err := r.gameDir(q.Get("library_id"), q.Get("id"))
	if err != nil {
		http.NotFound(w, req)
		return
	}
	icon := q.Get("icon")
	if icon == "" || !filepath.IsLocal(filepath.FromSlash(icon)) || !strings.Contains(".png.jpg.jpeg.webp.ico", strings.ToLower(path.Ext(icon))) {
		http.NotFound(w, req)
		return
	}
	full := filepath.Join(dir, filepath.FromSlash(icon))
	if info, err := os.Lstat(full); err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, req, full)
}

// handleGameLaunch 启动原生游戏：按类型识别主程序并生成命令（原生 / Proton / Lime3DS / Ren'Py），
// 放进独立 systemd scope。同一时间只运行一个原生游戏。
func (r *Router) handleGameLaunch(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !r.gameControlAllowed(w, req) || !r.checkSectionAccess(w, req, "games", auth.TargetGUI) {
		return
	}
	user := auth.UserFromContext(req.Context())
	// 引擎存档还没有按账号重定向，普通账号启动会写到管理员的存档里（规格 4.4 未完成）。
	if user.Role != auth.RoleAdmin {
		http.Error(w, "per-user game saves are not implemented yet", 403)
		return
	}
	var input struct {
		LibraryID string `json:"library_id"`
		ID        string `json:"id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	category, dir, err := r.gameDir(input.LibraryID, input.ID)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	nativeOK := false
	for _, c := range games.NativeCategories {
		nativeOK = nativeOK || c == category
	}
	if !nativeOK {
		http.Error(w, "web games run in the web client", 403)
		return
	}
	meta := games.ReadMeta(dir)
	exe, err := games.FindExecutable(dir, category, meta)
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	argv, env, err := games.Command(dir, category, exe, meta, r.gameTools)
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	logsDir := filepath.Join(filepath.Dir(filepath.Dir(dir)), "logs")
	if lib := r.findLibrary(input.LibraryID); lib == nil || lib.ReadOnly {
		logsDir = filepath.Join(r.stateDir, "game-logs")
	}
	key := input.LibraryID + ":" + input.ID
	status, err := r.gameRuns.Start(req.Context(), key, games.LaunchOptions{
		GameDir: dir, WorkDir: filepath.Dir(filepath.Join(dir, filepath.FromSlash(exe))), Argv: argv, Env: env,
		Scope: fmt.Sprintf("app-omnihub-game-%d", time.Now().UnixNano()), UserID: user.ID, IsAdmin: true, LogsDir: logsDir,
	})
	if errors.Is(err, games.ErrRunBusy) {
		http.Error(w, "a native game is already running", 409)
		return
	}
	if err != nil {
		http.Error(w, "launch failed: "+err.Error(), 422)
		return
	}
	writeJSON(w, http.StatusCreated, status)
}
