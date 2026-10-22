package server

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"omnihub/internal/auth"
	"omnihub/internal/games"
	"omnihub/internal/library"
	"omnihub/internal/userstate"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func webSessionHash(req *http.Request) string { return auth.CredentialHash(req.Context()) }

func (r *Router) handleWebGameOpen(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if auth.ClientFromContext(req.Context()) != auth.TargetWeb {
		http.Error(w, "web client required", 403)
		return
	}
	select {
	case r.webGameOpens <- struct{}{}:
		defer func() { <-r.webGameOpens }()
	default:
		http.Error(w, "web game entry reads busy", 503)
		return
	}
	var input struct {
		LibraryID string `json:"library_id"`
		ID        string `json:"id"`
		ClientID  string `json:"client_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.LibraryID == "" || len(input.LibraryID) > 128 || input.ID == "" || len(input.ID) > 512 || input.ClientID == "" || len(input.ClientID) > 128 {
		http.Error(w, "invalid web game identity", 400)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "unexpected request data", 400)
		return
	}
	var selected *library.Library
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.ID == input.LibraryID {
			selected = lib
			break
		}
	}
	if selected == nil {
		http.Error(w, "library not found", 404)
		return
	}
	if !selected.IsOnline {
		http.Error(w, "library offline", 404)
		return
	}
	// 与游戏清单同一套模型：游戏 = 分类目录下的一个子目录（不再读旧的 games.db 索引；
	// 那个文件现在也用来缓存封面，没有旧索引的表）。
	gameCategory, gameDir, err := r.gameDir(selected.ID, input.ID)
	if err != nil {
		http.Error(w, "game not found", 404)
		return
	}
	webCategory := false
	for _, c := range webGameCategories {
		webCategory = webCategory || c == gameCategory
	}
	if !webCategory {
		http.Error(w, "native game unavailable to web client", 403)
		return
	}
	category := "standalone_games/" + gameCategory + "_games"
	if r.libMgr.IsCategoryDisabled(category) {
		http.Error(w, "game category disabled", 403)
		return
	}
	relative := gameCategory + "_games/" + filepath.Base(gameDir)
	kind, entry, err := games.WebEntry(gameDir, &games.Game{Target: "web_only", Category: gameCategory})
	if err != nil {
		http.Error(w, "web entry unavailable: "+err.Error(), 422)
		return
	}
	folder, err := os.Lstat(gameDir)
	if err != nil {
		http.Error(w, "game directory unavailable", 503)
		return
	}
	user := auth.UserFromContext(req.Context())
	sessionHash := webSessionHash(req)
	if user.ID > 0 && sessionHash == "" {
		http.Error(w, "web session required", 403)
		return
	}
	if err := req.Context().Err(); err != nil {
		http.Error(w, "web open cancelled", 503)
		return
	}
	grant, err := r.webGames.issue(webGameGrant{libraryID: selected.ID, relative: relative, category: category, sessionHash: sessionHash, deviceCredential: auth.AuthenticationMethod(req.Context()) == "device", deviceID: auth.DeviceID(req.Context()), userID: user.ID, folder: folder, clientID: input.ClientID})
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	encoded := make([]string, 0)
	for _, part := range strings.Split(entry, "/") {
		encoded = append(encoded, url.PathEscape(part))
	}
	w.Header().Set("Cache-Control", "no-store")
	out := map[string]any{"token": grant.token, "kind": kind, "url": "/game-content/" + grant.token + "/" + strings.Join(encoded, "/"), "expires_at": grant.expires}
	if kind == "retro" {
		system := retroSystem(filepath.Base(gameDir))
		out["system"] = system
		if retroUnsupported(system, entry) {
			out["unsupported"] = true
		}
		if bios := retroBios(system, entry); bios != "" {
			if _, ok := r.retroBiosPath(selected.ID, bios); ok {
				out["bios"] = "/game-content/" + grant.token + "/__bios/" + bios
			} else {
				out["bios_missing"] = bios
			}
		}
	}
	writeJSON(w, 201, out)
}

func (r *Router) handleWebGameAsset(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS, DELETE, POST")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	if req.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(req.URL.Path, "/game-content/"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || len(parts[0]) > 128 {
		http.Error(w, "invalid game grant", 400)
		return
	}
	// Possession already authorizes this single game's public assets. Releasing
	// that capability requires no account cookie, so late cleanup after an account
	// switch cannot leave an old privileged grant active.
	if req.Method == http.MethodDelete && parts[1] == "" {
		r.webGames.revoke(parts[0])
		w.WriteHeader(204)
		return
	}
	saveFile, isSaveFile := strings.CutPrefix(parts[1], "__savefile/")
	isSaveFile = isSaveFile && saveFile != "" && !strings.ContainsAny(saveFile, "/\\\x00") && len(saveFile) <= 255
	isSave := parts[1] == "__save" || isSaveFile
	isList := parts[1] == "__list"
	if req.Method != http.MethodGet && req.Method != http.MethodHead && !(isSave && req.Method == http.MethodPost) {
		http.Error(w, "method not allowed", 405)
		return
	}
	biosName, isBios := strings.CutPrefix(parts[1], "__bios/")
	if isBios && !retroBiosFiles[biosName] || !isBios && !isSave && !isList && !games.PublicWebAsset(parts[1]) {
		http.Error(w, "invalid game asset", 400)
		return
	}
	grant, ok := r.webGames.get(parts[0])
	if !ok {
		http.Error(w, "web game expired or closed", 403)
		return
	}
	level := auth.LevelGuest
	if grant.userID > 0 {
		var user *auth.User
		var err error
		if grant.deviceCredential {
			if !auth.IsDirectLAN(req) {
				http.Error(w, "remembered device game requires direct LAN", 403)
				return
			}
			user, _, err = r.authDB.TrustedUserByHash(req.Context(), grant.sessionHash)
		} else {
			user, err = r.authDB.GetUserBySessionHash(grant.sessionHash)
		}
		if err != nil {
			http.Error(w, "session store unavailable", 503)
			return
		}
		if user == nil || user.ID != grant.userID {
			r.webGames.revoke(grant.token)
			http.Error(w, "web game session revoked", 403)
			return
		}
		level = user.Level
	}
	required, err := r.authDB.GetSectionRequiredLevel("games")
	if err != nil {
		http.Error(w, "game permission unavailable", 503)
		return
	}
	if level < required || !r.sectionEnabled("games") || r.libMgr.IsCategoryDisabled(grant.category) {
		http.Error(w, "game access disabled", 403)
		return
	}
	if isSaveFile {
		r.serveWebGameSaveFile(w, req, grant, saveFile)
		return
	}
	if isSave {
		r.serveWebGameSave(w, req, grant)
		return
	}
	if isBios {
		p, ok := r.retroBiosPath(grant.libraryID, biosName)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		http.ServeFile(w, req, p)
		return
	}
	gameDir, err := r.libMgr.ResolveCategoryPath(grant.libraryID+":standalone_games/"+grant.relative, grant.category)
	if err != nil {
		http.Error(w, "game library unavailable", 503)
		return
	}
	folder, err := os.Lstat(gameDir)
	if err != nil || !os.SameFile(grant.folder, folder) {
		http.Error(w, "game directory changed", 409)
		return
	}
	select {
	case r.webGameReads <- struct{}{}:
		defer func() { <-r.webGameReads }()
	default:
		http.Error(w, "game asset reads busy", 503)
		return
	}
	root, err := os.OpenRoot(gameDir)
	if err != nil {
		http.Error(w, "game assets unavailable", 503)
		return
	}
	defer func() {
		if err := root.Close(); err != nil {
			log.Printf("close web game asset root: %v", err)
		}
	}()
	openedFolder, err := root.Stat(".")
	if err != nil || !os.SameFile(grant.folder, openedFolder) {
		http.Error(w, "game directory changed during open", 409)
		return
	}
	if isList {
		writeJSON(w, http.StatusOK, games.ListWebDir(root, req.URL.Query().Get("path")))
		return
	}
	file, err := games.OpenWebAsset(root, parts[1])
	if err != nil {
		http.NotFound(w, req)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("close web game asset: %v", err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "game asset stat failed", 503)
		return
	}
	if ext := strings.ToLower(path.Ext(parts[1])); ext == ".html" || ext == ".htm" {
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-pointer-lock allow-modals allow-forms allow-downloads; default-src * data: blob:; script-src * 'unsafe-inline' 'unsafe-eval' data: blob:; frame-ancestors 'self'; base-uri 'none'")
		if info.Size() <= maxGamePageBytes {
			page, err := io.ReadAll(io.LimitReader(file, maxGamePageBytes))
			if err != nil {
				http.Error(w, "game asset read failed", 503)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, req, filepath.Base(parts[1]), info.ModTime(), bytes.NewReader(withGameRuntime(page, strings.HasSuffix(grant.category, "/rpg_games"))))
			return
		}
	}
	http.ServeContent(w, req, filepath.Base(parts[1]), info.ModTime(), file)
}

const maxGamePageBytes = 1 << 20

// 游戏 HTML 的 <head> 最前面注入 game_runtime.js（沙箱补丁：图片 CORS、Worker、存档、adapter.js）；
// RPG 游戏再加 rpg_runtime.js（NW.js 环境模拟，移植自 omni-deck），adapter.js 由它加载。
//
//go:embed game_runtime.js
var gameRuntime string

//go:embed rpg_runtime.js
var rpgRuntime string

func withGameRuntime(page []byte, rpg bool) []byte {
	inject := "<script>" + gameRuntime + "</script>"
	if rpg {
		inject = "<script>window.__omniRpg=true;\n" + gameRuntime + "</script><script>" + rpgRuntime + "</script>"
	}
	lower := bytes.ToLower(page)
	at := 0
	if i := bytes.Index(lower, []byte("<head")); i >= 0 {
		if j := bytes.IndexByte(page[i:], '>'); j >= 0 {
			at = i + j + 1
		}
	} else if i := bytes.Index(lower, []byte("<html")); i >= 0 {
		if j := bytes.IndexByte(page[i:], '>'); j >= 0 {
			at = i + j + 1
		}
	}
	out := make([]byte, 0, len(page)+len(inject))
	return append(append(append(out, page[:at]...), inject...), page[at:]...)
}

// serveWebGameSave：GET 返回本账号该游戏的全部存档（访客为空）；POST {"key","value"}（value 为 null 时删除）。
// 存档按授权里的账号与游戏目录记，游戏脚本拿不到账号凭证。
func (r *Router) serveWebGameSave(w http.ResponseWriter, req *http.Request, grant webGameGrant) {
	w.Header().Set("Cache-Control", "no-store")
	if grant.userID <= 0 || r.userState == nil {
		if req.Method == http.MethodPost {
			http.Error(w, "guest saves stay in this page", http.StatusForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{})
		return
	}
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		saves, err := r.userState.WebGameSaves(req.Context(), grant.userID, grant.relative)
		if err != nil {
			http.Error(w, "save storage unavailable", 503)
			return
		}
		writeJSON(w, http.StatusOK, saves)
		return
	}
	var in struct {
		Key   string  `json:"key"`
		Value *string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, userstate.MaxWebSaveValue+4096)).Decode(&in); err != nil {
		http.Error(w, "invalid save", 400)
		return
	}
	value := ""
	if in.Value != nil {
		value = *in.Value
	}
	if err := r.userState.SetWebGameSave(req.Context(), grant.userID, grant.relative, in.Key, value, in.Value == nil); err != nil {
		status := 503
		if errors.Is(err, userstate.ErrWebSaveTooLarge) {
			status = http.StatusRequestEntityTooLarge
		} else if errors.Is(err, userstate.ErrInvalidProgress) {
			status = 400
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveWebGameSaveFile：rpg_runtime 的存档文件（file1.rpgsave、config.rmmzsave…）。GET/HEAD 读，POST 原文写入；
// 与 __save 同一张表，键为 "file:<文件名>"。访客读不到、写入被拒（运行时那边仍留在本页内存里）。
func (r *Router) serveWebGameSaveFile(w http.ResponseWriter, req *http.Request, grant webGameGrant, name string) {
	w.Header().Set("Cache-Control", "no-store")
	key := "file:" + name
	if grant.userID <= 0 || r.userState == nil {
		if req.Method == http.MethodPost {
			http.Error(w, "guest saves stay in this page", http.StatusForbidden)
		} else {
			http.NotFound(w, req)
		}
		return
	}
	if req.Method == http.MethodPost {
		data, err := io.ReadAll(http.MaxBytesReader(w, req.Body, userstate.MaxWebSaveValue))
		if err != nil {
			http.Error(w, "save too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := r.userState.SetWebGameSave(req.Context(), grant.userID, grant.relative, key, string(data), false); err != nil {
			status := 503
			if errors.Is(err, userstate.ErrWebSaveTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	saves, err := r.userState.WebGameSaves(req.Context(), grant.userID, grant.relative)
	if err != nil {
		http.Error(w, "save storage unavailable", 503)
		return
	}
	value, ok := saves[key]
	if !ok {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if req.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(len(value)))
		return
	}
	_, _ = io.WriteString(w, value)
}
