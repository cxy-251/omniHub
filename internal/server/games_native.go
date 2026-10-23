package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
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
	"omnihub/internal/userstate"
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
	Kind         string    `json:"kind,omitempty"` // "sc2" 表示星际争霸 2 对战面板
	Favorite     bool      `json:"favorite,omitempty"`
	PlayTimeSec  int64     `json:"play_time_sec,omitempty"`
	LastPlayedAt time.Time `json:"last_played_at,omitzero"`
	// Profile 是硬件画像（游戏在自己的 scope 里跑过才有）
	Profile *userstate.GameProfile `json:"profile,omitempty"`
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
	// 星际争霸 2 不在资源库里，作为 Wine 分类的一张卡片（规格 10.1）。
	if r.sc2Available() && auth.ClientFromContext(req.Context()) == auth.TargetGUI && (want == "" || want == "wine") {
		out = append(out, gameEntry{Entry: games.Entry{ID: sc2GameID, Name: "星际争霸 II", Category: "wine", Folder: "StarCraft II"}, Kind: "sc2"})
	}
	// 账号收藏与游玩时长（时长按 100 个一批查询）。
	if user.ID > 0 && r.userState != nil {
		if fav, err := r.userState.GameFavorites(req.Context(), user.ID); err == nil {
			for i := range out {
				out[i].Favorite = fav[out[i].ID]
			}
		}
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
			if profiles, err := r.userState.GameProfiles(req.Context(), ids); err == nil {
				for i := range batch {
					if p, ok := profiles[batch[i].ID]; ok {
						batch[i].Profile = &p
					}
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

// handleGameFavorite 收藏或取消收藏：{"id": "分类/目录名", "favorite": true}。
func (r *Router) handleGameFavorite(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var input struct {
		ID       string `json:"id"`
		Favorite bool   `json:"favorite"`
	}
	user := auth.UserFromContext(req.Context())
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil || user.ID <= 0 {
		http.Error(w, "invalid request or not logged in", 400)
		return
	}
	if err := r.userState.SetGameFavorite(req.Context(), user.ID, input.ID, input.Favorite); err != nil {
		http.Error(w, "favorite storage unavailable", 503)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"favorite": input.Favorite})
}

// gameArtStore 返回可写资源库的封面缓存（standalone_games/games.db）；只读库返回 nil。
func (r *Router) gameArtStore(libID string) *games.ArtStore {
	lib := r.findLibrary(libID)
	if lib == nil || lib.ReadOnly || !lib.IsOnline {
		return nil
	}
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if r.closed {
		return nil
	}
	if s := r.gameArt[lib.RootPath]; s != nil {
		return s
	}
	dir, err := r.libMgr.ResolveCategoryPath(lib.ID+":standalone_games", "standalone_games")
	if err != nil {
		return nil
	}
	s, err := games.OpenArtStore(dir)
	if err != nil {
		return nil
	}
	r.gameArt[lib.RootPath] = s
	return s
}

// handleGameArt 返回游戏封面：omni.json/图片文件 → Steam 封面缓存 → exe 内嵌图标；结果缓存在 games.db。
// 没有可用图片时返回 1×1 透明图（客户端显示生成的卡片）。
func (r *Router) handleGameArt(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	libID, id := q.Get("library_id"), q.Get("id")
	if id == sc2GameID {
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.ServeFile(w, req, filepath.Join(r.gameTools.SC2Dir, "icon.png"))
		return
	}
	category, dir, err := r.gameDir(libID, id)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	store := r.gameArtStore(libID)
	var art games.Art
	if data, mime, ok := func() ([]byte, string, bool) {
		if store == nil {
			return nil, "", false
		}
		return store.Cached(id)
	}(); ok {
		art = games.Art{MIME: mime, Data: data}
	} else {
		entries, _ := games.ScanCategory(filepath.Dir(dir), category, libID)
		icon := ""
		for _, e := range entries {
			if e.ID == id {
				icon = e.Icon
			}
		}
		art, err = games.FindArt(dir, category, games.ReadMeta(dir), icon, r.gameTools.SteamRoot)
		if err != nil {
			art = games.Art{Source: dir, MIME: "", Data: []byte{}} // 记下「没有图」，目录变化前不再重算
		}
		if store != nil {
			_ = store.Put(id, art)
		}
	}
	if len(art.Data) == 0 {
		writeNoArt(w)
		return
	}
	w.Header().Set("Content-Type", art.MIME)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(art.Data)
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
	var exe string
	var argv, env []string
	if category == "retro" {
		exe, argv, err = r.retroCommand(input.LibraryID, dir, meta)
	} else if exe, err = games.FindExecutable(dir, category, meta); err == nil {
		argv, env, err = games.Command(dir, category, exe, meta, r.gameTools)
	}
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	logsDir := filepath.Join(filepath.Dir(filepath.Dir(dir)), "logs")
	if lib := r.findLibrary(input.LibraryID); lib == nil || lib.ReadOnly {
		logsDir = filepath.Join(r.cacheDir, "game-logs")
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

// noArtPNG 是 1×1 透明 PNG。没有封面时返回它而不是 404：客户端按尺寸判断显示名字卡片，
// 不会把「没有封面」当成请求错误反复记日志。
var noArtPNG = func() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	return buf.Bytes()
}()

func writeNoArt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(noArtPNG)
}
