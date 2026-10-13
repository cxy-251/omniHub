package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"omnihub/internal/audio"
	"omnihub/internal/auth"
	"omnihub/internal/gallery"
	"omnihub/internal/games"
	"omnihub/internal/library"
	"omnihub/internal/manga"
	"omnihub/internal/mega"
	"omnihub/internal/sc2"
	"omnihub/internal/shortvideo"
	"omnihub/internal/tasks"
	"omnihub/internal/text"
	"omnihub/internal/tools"
	"omnihub/web"
)

// Router 负责 HTTP 请求的统一路由分发与鉴权控制。
type Router struct {
	mux        *http.ServeMux
	libMgr     *library.Manager
	authDB     *auth.DB
	authMid    *auth.Middleware
	clipboard  *tools.ClipboardManager
	taskMgr    *tasks.Manager
	progressDB *audio.ProgressDB
	megaClient *mega.Client

	// 专区数据库连接复用缓存（第 12.2 节避免高频请求重复 Open 与 Migrate）
	dbMu      sync.RWMutex
	closed    bool
	metaDB    map[string]*gallery.MetaDB
	svStorage map[string]*shortvideo.Storage
	mangaDB   map[string]*manga.DB
	gamesDB   map[string]*games.DB
}

// NewRouter 创建路由分发器实例。
func NewRouter(libMgr *library.Manager, authDB *auth.DB, stateDir, localToken string) (*Router, error) {
	// 初始化音频断点数据库
	if stateDir == "" {
		return nil, errors.New("router state directory is required")
	}
	audioDBPath := filepath.Join(stateDir, "audio_progress.db")
	progDB, err := audio.OpenProgressDB(audioDBPath)
	if err != nil {
		return nil, fmt.Errorf("open audio progress: %w", err)
	}

	r := &Router{
		mux:        http.NewServeMux(),
		metaDB:     make(map[string]*gallery.MetaDB),
		svStorage:  make(map[string]*shortvideo.Storage),
		mangaDB:    make(map[string]*manga.DB),
		gamesDB:    make(map[string]*games.DB),
		libMgr:     libMgr,
		authDB:     authDB,
		authMid:    auth.NewMiddleware(authDB, localToken),
		clipboard:  tools.NewClipboardManager(50),
		taskMgr:    tasks.NewManager(),
		progressDB: progDB,
		megaClient: mega.NewClient("", 5*time.Minute),
	}
	r.registerRoutes()
	return r, nil
}

func (r *Router) getMetaDB(root string) (*gallery.MetaDB, error) {
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if r.closed {
		return nil, errors.New("router is closed")
	}
	readOnly := r.libMgr.IsReadOnly(root)
	key := fmt.Sprintf("%s:%t", root, readOnly)
	if db := r.metaDB[key]; db != nil {
		return db, nil
	}
	galleryDir := filepath.Join(root, "media_library", "gallery")
	open := gallery.OpenMetaDB
	if readOnly {
		open = gallery.OpenMetaDBReadOnly
	}

	metaDB, err := open(galleryDir)
	if err != nil {
		return nil, err
	}
	r.metaDB[key] = metaDB
	return metaDB, nil
}

func (r *Router) getSVStorage(root string) (*shortvideo.Storage, error) {
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if r.closed {
		return nil, errors.New("router is closed")
	}
	readOnly := r.libMgr.IsReadOnly(root)
	key := fmt.Sprintf("%s:%t", root, readOnly)
	if db := r.svStorage[key]; db != nil {
		return db, nil
	}
	svDir := filepath.Join(root, "media_library", "shortvideo")
	open := shortvideo.OpenStorage
	if readOnly {
		open = shortvideo.OpenStorageReadOnly
	}

	storage, err := open(svDir)
	if err != nil {
		return nil, err
	}
	r.svStorage[key] = storage
	return storage, nil
}

func (r *Router) getMangaDB(root string) (*manga.DB, error) {
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if r.closed {
		return nil, errors.New("router is closed")
	}
	readOnly := r.libMgr.IsReadOnly(root)
	key := fmt.Sprintf("%s:%t", root, readOnly)
	if db := r.mangaDB[key]; db != nil {
		return db, nil
	}
	mangaDir := filepath.Join(root, "media_library", "manga")
	open := manga.OpenDB
	if readOnly {
		open = manga.OpenDBReadOnly
	}

	mDB, err := open(mangaDir)
	if err != nil {
		return nil, err
	}
	r.mangaDB[key] = mDB
	return mDB, nil
}

func (r *Router) getGamesDB(root string) (*games.DB, error) {
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if r.closed {
		return nil, errors.New("router is closed")
	}
	readOnly := r.libMgr.IsReadOnly(root)
	key := fmt.Sprintf("%s:%t", root, readOnly)
	if db := r.gamesDB[key]; db != nil {
		return db, nil
	}
	gamesDir := filepath.Join(root, "standalone_games")
	open := games.OpenDB
	if readOnly {
		open = games.OpenDBReadOnly
	}

	gdb, err := open(gamesDir)
	if err != nil {
		return nil, err
	}
	r.gamesDB[key] = gdb
	return gdb, nil
}

// Close 在 HTTP 服务完成退出后关闭所有模块连接，允许重复调用。
func (r *Router) Close() error {
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var errs []error
	for _, db := range r.metaDB {
		errs = append(errs, db.Close())
	}
	for _, db := range r.svStorage {
		errs = append(errs, db.Close())
	}
	for _, db := range r.mangaDB {
		errs = append(errs, db.Close())
	}
	for _, db := range r.gamesDB {
		errs = append(errs, db.Close())
	}
	errs = append(errs, r.progressDB.Close(), r.taskMgr.Close())
	return errors.Join(errs...)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.authMid.Handler(r.protectRoutes(r.mux)).ServeHTTP(w, req)
}

func (r *Router) registerRoutes() {
	// 基础状态与健康检查
	r.mux.HandleFunc("/api/health", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok"})
	})

	// 用户系统与状态接口
	r.mux.HandleFunc("/api/auth/status", r.handleAuthStatus)
	r.mux.HandleFunc("/api/sections", r.handleSections)
	r.mux.HandleFunc("/api/auth/setup", r.handleAuthSetup)
	r.mux.HandleFunc("/api/auth/login", r.handleAuthLogin)

	// 媒体库管理接口
	r.mux.HandleFunc("/api/library/list", r.handleLibraryList)

	// 画廊接口
	r.mux.HandleFunc("/api/media/gallery/list", r.handleGalleryList)
	r.mux.HandleFunc("/api/media/gallery/thumbnail", r.handleGalleryThumbnail)
	r.mux.HandleFunc("/api/media/gallery/original", r.handleGalleryOriginal)
	r.mux.HandleFunc("/api/media/gallery/trash", r.handleGalleryTrash)

	// 短视频与图集接口
	r.mux.HandleFunc("/api/media/shortvideo/stream", r.handleShortVideoStream)
	r.mux.HandleFunc("/api/media/shortvideo/albums", r.handleShortVideoAlbums)
	r.mux.HandleFunc("/api/media/shortvideo/album/image", r.handleShortVideoAlbumImage)
	r.mux.HandleFunc("/api/media/shortvideo/thumbnail", r.handleShortVideoThumbnail)
	r.mux.HandleFunc("/api/media/shortvideo/channels", r.handleShortVideoChannels)
	r.mux.HandleFunc("/api/media/shortvideo/random", r.handleShortVideoRandom)

	// 文本业务接口
	r.mux.HandleFunc("/api/media/text/list", r.handleTextList)
	r.mux.HandleFunc("/api/media/text/chunk", r.handleTextChunk)
	r.mux.HandleFunc("/api/media/text/epub", r.handleTextEpub)

	// 音频业务接口
	r.mux.HandleFunc("/api/media/audio/albums", r.handleAudioAlbums)
	r.mux.HandleFunc("/api/media/audio/album", r.handleAudioAlbum)
	r.mux.HandleFunc("/api/media/audio/stream", r.handleAudioStream)
	r.mux.HandleFunc("/api/media/audio/progress", r.handleAudioProgress)

	// 漫画业务接口
	r.mux.HandleFunc("/api/media/manga/list", r.handleMangaList)
	r.mux.HandleFunc("/api/media/manga/pages", r.handleMangaPages)
	r.mux.HandleFunc("/api/media/manga/cover", r.handleMangaCover)
	r.mux.HandleFunc("/api/media/manga/page", r.handleMangaPage)
	r.mux.HandleFunc("/api/media/manga/progress", r.handleMangaProgress)

	// 游戏中心接口（双端过滤）
	r.mux.HandleFunc("/api/games/list", r.handleGamesList)

	// 任务调试台接口
	r.mux.HandleFunc("/api/tasks/run", r.handleTasksRun)
	r.mux.HandleFunc("/api/tasks/stop", r.handleTasksStop)
	r.mux.HandleFunc("/api/tasks/logs", r.handleTasksLogs)

	// 权限管理 Admin 接口
	r.mux.HandleFunc("/api/admin/permissions", r.handleAdminPermissions)
	r.mux.HandleFunc("/api/admin/users", r.handleAdminUsers)

	// 局域网小工具接口
	r.mux.HandleFunc("/api/tools/clipboard", r.handleToolsClipboard)
	r.mux.HandleFunc("/api/tools/metrics", r.handleToolsMetrics)
	r.mux.HandleFunc("/api/tools/wol", r.handleToolsWOL)
	r.mux.HandleFunc("/api/tools/lanscan", r.handleToolsLANScan)
	r.mux.HandleFunc("/api/tools/shared/files", r.handleToolsSharedFiles)
	r.mux.HandleFunc("/api/tools/shared/upload", r.handleToolsSharedUpload)
	r.mux.HandleFunc("/api/tools/shared/zip", r.handleToolsSharedZip)
	r.mux.HandleFunc("/api/tools/speed", r.handleToolsSpeed)
	r.mux.HandleFunc("/api/tools/webshell", r.handleToolsWebShell)

	// 星际争霸 2 定制接口 (第 10.1 节)
	r.mux.HandleFunc("/api/sc2/maps", r.handleSC2Maps)
	r.mux.HandleFunc("/api/sc2/launch", r.handleSC2Launch)

	// MEGA 客户端接口 (第 10.2 节)
	r.mux.HandleFunc("/api/mega/status", r.handleMegaStatus)
	r.mux.HandleFunc("/api/mega/download", r.handleMegaDownload)

	// Web 游戏静态资源服务（第 4.2 节）
	r.mux.HandleFunc("/standalone_games/", func(w http.ResponseWriter, req *http.Request) {
		primary := r.libMgr.GetPrimary()
		if primary == nil {
			http.NotFound(w, req)
			return
		}
		subPath := strings.TrimPrefix(req.URL.Path, "/standalone_games/")
		parts := strings.Split(subPath, "/")
		if len(parts) < 2 || (parts[0] != "rpg_games" && parts[0] != "slg_games" && parts[0] != "retro_games" && parts[0] != "flash_games") {
			http.Error(w, "web game asset not allowed", http.StatusForbidden)
			return
		}
		for _, part := range parts {
			if part == "" || strings.HasPrefix(part, ".") || strings.EqualFold(part, "save") || strings.EqualFold(part, "saves") || strings.EqualFold(part, "logs") || strings.EqualFold(part, "omni.json") {
				http.Error(w, "private game asset not allowed", http.StatusForbidden)
				return
			}
		}
		fullPath, err := r.libMgr.ResolveCategoryPath(primary.ID+":standalone_games/"+subPath, "standalone_games/"+parts[0])
		if err != nil {
			http.Error(w, "invalid game asset path", http.StatusBadRequest)
			return
		}
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			http.NotFound(w, req)
			return
		}
		http.ServeFile(w, req, fullPath)
	})

	// 嵌入式 Web 根路由静态文件
	r.mux.Handle("/", http.FileServer(http.FS(web.FS)))
}

// --- 鉴权与媒体库 ---

func (r *Router) handleAuthStatus(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	hasAdmin, err := r.authDB.HasAdmin()
	if err != nil {
		http.Error(w, "authentication store unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logged_in": user.ID > 0,
		"user":      user,
		"has_admin": hasAdmin,
	})
}

func (r *Router) handleAuthSetup(w http.ResponseWriter, req *http.Request) {
	if !auth.IsTrustedLocalFromContext(req.Context()) {
		http.Error(w, "administrator setup requires trusted local client", http.StatusForbidden)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hasAdmin, _ := r.authDB.HasAdmin()
	if hasAdmin {
		http.Error(w, "admin already exists", http.StatusForbidden)
		return
	}

	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || payload.Username == "" || payload.Password == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, err := r.authDB.CreateInitialAdmin(payload.Username, payload.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	token, err := auth.GenerateSecureToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := r.authDB.CreateSession(user.ID, token, 24*time.Hour); err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "omni_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   req.TLS != nil,
		MaxAge:   86400,
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"token":  token,
		"user":   user,
	})
}

func (r *Router) handleAuthLogin(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, err := r.authDB.Authenticate(req.RemoteAddr, payload.Username, payload.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	token, err := auth.GenerateSecureToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := r.authDB.CreateSession(user.ID, token, 24*time.Hour); err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "omni_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   req.TLS != nil,
		MaxAge:   86400,
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"token":  token,
		"user":   user,
	})
}

func (r *Router) handleLibraryList(w http.ResponseWriter, req *http.Request) {
	libs := r.libMgr.ListLibraries()
	primary := r.libMgr.GetPrimary()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"libraries": libs,
		"primary":   primary,
	})
}

// --- 画廊模块 ---

func (r *Router) handleGalleryList(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "gallery", auth.TargetAll) {
		return
	}

	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	metaDB, err := r.getMetaDB(primary.RootPath)
	if err != nil {
		http.Error(w, "module database unavailable", http.StatusServiceUnavailable)
		return
	}

	incDeleted := req.URL.Query().Get("include_deleted") == "1"
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	photos, err := metaDB.ListPhotos(incDeleted, limit, offset)
	if err != nil {
		http.Error(w, "module database unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, photos)
}

func (r *Router) handleGalleryThumbnail(w http.ResponseWriter, req *http.Request) {
	photoID := req.URL.Query().Get("id")
	primary := r.libMgr.GetPrimary()
	if primary == nil || photoID == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	metaDB, err := r.getMetaDB(primary.RootPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	thumb, err := metaDB.GetThumbnail(photoID)
	if err != nil || len(thumb) == 0 {
		http.Error(w, "thumbnail not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(thumb)
}

func (r *Router) handleGalleryOriginal(w http.ResponseWriter, req *http.Request) {
	photoID := req.URL.Query().Get("id")
	vol := req.URL.Query().Get("vol")
	primary := r.libMgr.GetPrimary()
	if primary == nil || photoID == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	originalsDir := filepath.Join(primary.RootPath, "media_library", "gallery", "originals")
	data, err := gallery.ReadOriginalDirect(originalsDir, vol, photoID)
	if err != nil || len(data) == 0 {
		http.Error(w, "original image not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (r *Router) handleGalleryTrash(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	photoID := req.URL.Query().Get("id")
	restore := req.URL.Query().Get("restore") == "1"
	primary := r.libMgr.GetPrimary()
	if primary == nil || photoID == "" {
		http.Error(w, "not found", http.StatusBadRequest)
		return
	}
	metaDB, err := r.getMetaDB(primary.RootPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = metaDB.SoftDelete(photoID, !restore)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- 短视频与图集 ---

func (r *Router) handleShortVideoStream(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}

	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/shortvideo")
	if err != nil {
		http.Error(w, "file not found in library", http.StatusNotFound)
		return
	}

	if strings.ToLower(filepath.Ext(fullPath)) != ".mp4" {
		http.Error(w, "unsupported media format", http.StatusBadRequest)
		return
	}

	_ = shortvideo.StreamVideo(w, req, fullPath)
}

func (r *Router) handleShortVideoAlbums(w http.ResponseWriter, req *http.Request) {
	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, map[string]int{})
		return
	}
	storage, err := r.getSVStorage(primary.RootPath)
	if err != nil {
		http.Error(w, "module database unavailable", http.StatusServiceUnavailable)
		return
	}

	albums, err := storage.ListAlbums()
	if err != nil {
		http.Error(w, "album query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, albums)
}

func (r *Router) handleShortVideoAlbumImage(w http.ResponseWriter, req *http.Request) {
	postID := req.URL.Query().Get("post_id")
	idx, _ := strconv.Atoi(req.URL.Query().Get("index"))
	primary := r.libMgr.GetPrimary()
	if primary == nil || postID == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	storage, err := r.getSVStorage(primary.RootPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	data, err := storage.GetAlbumImage(postID, idx)
	if err != nil || len(data) == 0 {
		http.Error(w, "image not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (r *Router) handleShortVideoThumbnail(w http.ResponseWriter, req *http.Request) {
	targetID := req.URL.Query().Get("id")
	primary := r.libMgr.GetPrimary()
	if primary == nil || targetID == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	storage, err := r.getSVStorage(primary.RootPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	data, err := storage.GetThumbnail(targetID)
	if err != nil || len(data) == 0 {
		http.Error(w, "thumbnail not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (r *Router) handleShortVideoChannels(w http.ResponseWriter, req *http.Request) {
	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	svDir := filepath.Join(primary.RootPath, "media_library", "shortvideo")
	entries, err := os.ReadDir(svDir)
	if err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	var channels []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			channels = append(channels, e.Name())
		}
	}
	writeJSON(w, http.StatusOK, channels)
}

func (r *Router) handleShortVideoRandom(w http.ResponseWriter, req *http.Request) {
	channel := req.URL.Query().Get("channel")
	primary := r.libMgr.GetPrimary()
	if primary == nil || channel == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	channelDir, err := r.libMgr.ResolveCategoryPath(primary.ID+":media_library/shortvideo/"+channel, "media_library/shortvideo")
	if err != nil {
		http.Error(w, "invalid channel", http.StatusBadRequest)
		return
	}

	var mp4Files []string
	_ = filepath.Walk(channelDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".mp4") {
			rel, _ := filepath.Rel(primary.RootPath, path)
			mp4Files = append(mp4Files, rel)
		}
		return nil
	})

	if len(mp4Files) == 0 {
		http.Error(w, "no videos in channel", http.StatusNotFound)
		return
	}

	idx := time.Now().UnixNano() % int64(len(mp4Files))
	writeJSON(w, http.StatusOK, map[string]string{
		"path": fmt.Sprintf("%s:%s", primary.ID, mp4Files[idx]),
	})
}

// --- 文本 ---

func (r *Router) handleTextList(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "text", auth.TargetAll) {
		return
	}

	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	type TextItem struct {
		Name    string `json:"name"`
		RelPath string `json:"rel_path"`
		Ext     string `json:"ext"`
		Size    int64  `json:"size"`
	}

	var items []TextItem
	targetDirs := []string{
		filepath.Join(primary.RootPath, "media_library", "novels"),
		filepath.Join(primary.RootPath, "media_library", "docs"),
	}

	validExts := map[string]bool{".txt": true, ".epub": true, ".md": true, ".rst": true}

	for _, dir := range targetDirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				ext := strings.ToLower(filepath.Ext(info.Name()))
				if validExts[ext] {
					rel, _ := filepath.Rel(primary.RootPath, path)
					items = append(items, TextItem{
						Name:    info.Name(),
						RelPath: rel,
						Ext:     ext,
						Size:    info.Size(),
					})
				}
			}
			return nil
		})
	}

	writeJSON(w, http.StatusOK, items)
}

func (r *Router) handleTextChunk(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	startLine, _ := strconv.Atoi(req.URL.Query().Get("start"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/docs", "media_library/novels")
	if err != nil {
		http.Error(w, "file not found in library", http.StatusNotFound)
		return
	}

	res, err := text.ReadTextChunk(fullPath, startLine, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (r *Router) handleTextEpub(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	entry := req.URL.Query().Get("entry")
	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/docs", "media_library/novels")
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	data, err := text.ReadEpubEntry(fullPath, entry)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data:")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/xhtml+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// --- 音频 ---

func (r *Router) handleAudioAlbums(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "audio", auth.TargetAll) {
		return
	}
	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, []*audio.Album{})
		return
	}
	audioRootDir := filepath.Join(primary.RootPath, "media_library", "audio")
	albums, err := audio.ScanAlbums(audioRootDir)
	if err != nil {
		writeJSON(w, http.StatusOK, []*audio.Album{})
		return
	}
	writeJSON(w, http.StatusOK, albums)
}

func (r *Router) handleAudioAlbum(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/audio")
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}

	album, err := audio.ScanAlbum(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, album)
}

func (r *Router) handleAudioStream(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/audio")
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if !audio.SupportedExtensions[strings.ToLower(filepath.Ext(fullPath))] {
		http.Error(w, "unsupported media format", http.StatusBadRequest)
		return
	}

	http.ServeFile(w, req, fullPath)
}

func (r *Router) handleAudioProgress(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	if r.progressDB == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"offset": 0})
		return
	}

	switch req.Method {
	case http.MethodGet:
		audioID := req.URL.Query().Get("audio_id")
		offset, _ := r.progressDB.GetProgress(user.ID, audioID)
		writeJSON(w, http.StatusOK, map[string]interface{}{"offset": offset})
	case http.MethodPost:
		var p struct {
			AudioID string  `json:"audio_id"`
			Offset  float64 `json:"offset"`
		}
		if err := json.NewDecoder(req.Body).Decode(&p); err == nil {
			_ = r.progressDB.SaveProgress(user.ID, p.AudioID, p.Offset)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- 漫画 ---

func (r *Router) handleMangaList(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "manga", auth.TargetAll) {
		return
	}

	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	mangaDB, err := r.getMangaDB(primary.RootPath)
	if err != nil {
		http.Error(w, "module database unavailable", http.StatusServiceUnavailable)
		return
	}

	list, err := mangaDB.ListMangas()
	if err != nil {
		http.Error(w, "manga query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (r *Router) handleMangaPages(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/manga")
	if err != nil {
		http.Error(w, "cbz not found", http.StatusNotFound)
		return
	}
	reader, err := manga.OpenCBZ(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	entries := reader.ListImageEntries()
	writeJSON(w, http.StatusOK, entries)
}

func (r *Router) handleMangaCover(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/manga")
	if err != nil {
		http.Error(w, "cbz not found", http.StatusNotFound)
		return
	}
	reader, err := manga.OpenCBZ(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	coverBytes, err := reader.ReadCoverBytes("")
	if err != nil {
		http.Error(w, "cover not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(coverBytes)
}

func (r *Router) handleMangaPage(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	entry := req.URL.Query().Get("entry")
	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/manga")
	if err != nil {
		http.Error(w, "cbz not found", http.StatusNotFound)
		return
	}
	reader, err := manga.OpenCBZ(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	data, err := reader.ReadEntry(entry)
	if err != nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (r *Router) handleMangaProgress(w http.ResponseWriter, req *http.Request) {
	primary := r.libMgr.GetPrimary()
	if primary == nil {
		http.Error(w, "no primary library", http.StatusBadRequest)
		return
	}
	user := auth.UserFromContext(req.Context())
	mangaDB, err := r.getMangaDB(primary.RootPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch req.Method {
	case http.MethodGet:
		mangaID := req.URL.Query().Get("manga_id")
		p, _ := mangaDB.GetProgress(user.ID, mangaID)
		writeJSON(w, http.StatusOK, p)
	case http.MethodPost:
		var p manga.MangaProgress
		if err := json.NewDecoder(req.Body).Decode(&p); err == nil {
			p.UserID = user.ID
			_ = mangaDB.SaveProgress(&p)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- 游戏专区 ---

func (r *Router) handleGamesList(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "games", auth.TargetAll) {
		return
	}

	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	gdb, err := r.getGamesDB(primary.RootPath)
	if err != nil {
		http.Error(w, "module database unavailable", http.StatusServiceUnavailable)
		return
	}

	// 双端物理隔离（第 4.2 节、第 5.2.2 节）：基于客户端标头强制约束，禁止客户端 query 篡改
	clientTarget := auth.ClientFromContext(req.Context())
	target := "web_only"
	if clientTarget == auth.TargetGUI {
		target = "gui_only"
	}

	list, err := gdb.ListGames(target)
	if err != nil {
		http.Error(w, "module database unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// --- 任务调试台 ---

func (r *Router) handleTasksRun(w http.ResponseWriter, req *http.Request) {
	if auth.IsWANFromContext(req.Context()) {
		http.Error(w, "forbidden: host-level operation blocked from WAN", http.StatusForbidden)
		return
	}
	user := auth.UserFromContext(req.Context())
	if user.Role != auth.RoleAdmin {
		http.Error(w, "forbidden: admin required", http.StatusForbidden)
		return
	}

	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID     string   `json:"id"`
		Script string   `json:"script"`
		Args   []string `json:"args"`
	}
	if err := json.NewDecoder(req.Body).Decode(&p); err != nil || p.Script == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.Contains(p.Script, "..") {
		http.Error(w, "invalid script path", http.StatusBadRequest)
		return
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("task_%d", time.Now().UnixNano())
	}

	task, err := r.taskMgr.RunScript(p.ID, p.Script, p.Args)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (r *Router) handleTasksStop(w http.ResponseWriter, req *http.Request) {
	if auth.IsWANFromContext(req.Context()) {
		http.Error(w, "forbidden: host-level operation blocked from WAN", http.StatusForbidden)
		return
	}
	user := auth.UserFromContext(req.Context())
	if user.Role != auth.RoleAdmin {
		http.Error(w, "forbidden: admin required", http.StatusForbidden)
		return
	}

	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&p); err != nil || p.ID == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := r.taskMgr.StopTask(p.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (r *Router) handleTasksLogs(w http.ResponseWriter, req *http.Request) {
	taskID := req.URL.Query().Get("id")
	task := r.taskMgr.GetTask(taskID)
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, history := task.SubscribeLogs(req.Context())
	for _, entry := range history {
		data, _ := json.Marshal(entry)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	for {
		select {
		case <-req.Context().Done():
			return
		case entry, open := <-ch:
			if !open {
				fmt.Fprintf(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- 权限管理 ---

func (r *Router) handleAdminPermissions(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	if user.Role != auth.RoleAdmin {
		http.Error(w, "forbidden: admin required", http.StatusForbidden)
		return
	}

	switch req.Method {
	case http.MethodGet:
		matrix, err := r.authDB.ListSectionPermissions()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, matrix)
	case http.MethodPost:
		var p struct {
			SectionID     string `json:"section_id"`
			RequiredLevel int    `json:"required_level"`
		}
		if err := json.NewDecoder(req.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := r.authDB.SetSectionRequiredLevel(p.SectionID, p.RequiredLevel); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleAdminUsers(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	if user.Role != auth.RoleAdmin {
		http.Error(w, "forbidden: admin required", http.StatusForbidden)
		return
	}

	switch req.Method {
	case http.MethodGet:
		users, err := r.authDB.ListUsers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, users)
	case http.MethodPost:
		var p struct {
			UserID int64 `json:"user_id"`
			Level  int   `json:"level"`
		}
		if err := json.NewDecoder(req.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := r.authDB.UpdateUserLevel(p.UserID, p.Level); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// 5.3.2 节：变更等级即时吊销所有已有会话 Token，防权限残留
		_ = r.authDB.RevokeUserSessions(p.UserID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- 局域网小工具 ---

func (r *Router) handleToolsClipboard(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, r.clipboard.List())
	case http.MethodPost:
		var p struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(req.Body).Decode(&p); err == nil && p.Content != "" {
			item := r.clipboard.Push(p.Content)
			writeJSON(w, http.StatusOK, item)
			return
		}
		http.Error(w, "invalid content", http.StatusBadRequest)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleToolsMetrics(w http.ResponseWriter, req *http.Request) {
	metrics := tools.GetSystemMetrics("/")
	writeJSON(w, http.StatusOK, metrics)
}

func (r *Router) handleToolsWOL(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var p struct {
		MAC string `json:"mac"`
	}
	if err := json.NewDecoder(req.Body).Decode(&p); err != nil || p.MAC == "" {
		http.Error(w, "invalid mac address", http.StatusBadRequest)
		return
	}

	if err := tools.SendWakeOnLAN(p.MAC); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleToolsLANScan(w http.ResponseWriter, req *http.Request) {
	devices, err := tools.ScanLAN()
	if err != nil {
		writeJSON(w, http.StatusOK, []tools.LANDevice{})
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

func (r *Router) handleToolsSharedFiles(w http.ResponseWriter, req *http.Request) {
	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	sharedDir := filepath.Join(primary.RootPath, "media_library", "shared")
	list, err := tools.ListSharedFiles(sharedDir)
	if err != nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (r *Router) handleToolsSharedUpload(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	primary := r.libMgr.GetPrimary()
	if primary == nil {
		http.Error(w, "no primary library", http.StatusBadRequest)
		return
	}
	sharedDir := filepath.Join(primary.RootPath, "media_library", "shared")

	chunkIndex, _ := strconv.Atoi(req.URL.Query().Get("chunk_index"))
	filename := req.URL.Query().Get("filename")
	if filename == "" {
		http.Error(w, "missing filename", http.StatusBadRequest)
		return
	}

	buf, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tools.WriteChunk(sharedDir, filename, chunkIndex, buf); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleToolsSharedZip(w http.ResponseWriter, req *http.Request) {
	primary := r.libMgr.GetPrimary()
	if primary == nil {
		http.Error(w, "no primary library", http.StatusBadRequest)
		return
	}
	sharedDir := filepath.Join(primary.RootPath, "media_library", "shared")
	filesParam := req.URL.Query().Get("files")
	var filenames []string
	if filesParam != "" {
		filenames = strings.Split(filesParam, ",")
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"shared_archive.zip\"")
	_ = tools.StreamZipArchive(sharedDir, filenames, w)
}

func (r *Router) handleToolsSpeed(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		// 下行测速：返回 5MB 随机数据
		w.Header().Set("Content-Type", "application/octet-stream")
		dummy := make([]byte, 1024*1024*5)
		_, _ = w.Write(dummy)
	case http.MethodPost:
		// 上行测速：读取上传流并丢弃
		_, _ = io.Copy(io.Discard, req.Body)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleToolsWebShell(w http.ResponseWriter, req *http.Request) {
	if auth.IsWANFromContext(req.Context()) {
		http.Error(w, "forbidden: host-level operation blocked from WAN", http.StatusForbidden)
		return
	}
	user := auth.UserFromContext(req.Context())
	if user.Role != auth.RoleAdmin {
		http.Error(w, "forbidden: admin only", http.StatusForbidden)
		return
	}

	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var p struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(req.Body).Decode(&p); err != nil || p.Command == "" {
		http.Error(w, "invalid command", http.StatusBadRequest)
		return
	}

	cmd := exec.Command("sh", "-c", p.Command)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	_ = cmd.Run()

	writeJSON(w, http.StatusOK, map[string]string{
		"output": outBuf.String(),
	})
}

// --- 星际争霸 2 定制接口 (第 10.1 节) ---

func (r *Router) handleSC2Maps(w http.ResponseWriter, req *http.Request) {
	primary := r.libMgr.GetPrimary()
	if primary == nil {
		writeJSON(w, http.StatusOK, []sc2.MapItem{})
		return
	}
	mapsDir := filepath.Join(primary.RootPath, "standalone_games", "wine_games", "StarCraft II", "Maps")
	maps, err := sc2.ListMaps(mapsDir)
	if err != nil {
		writeJSON(w, http.StatusOK, []sc2.MapItem{})
		return
	}
	writeJSON(w, http.StatusOK, maps)
}

func (r *Router) handleSC2Launch(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var opt sc2.MatchOptions
	if err := json.NewDecoder(req.Body).Decode(&opt); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	primary := r.libMgr.GetPrimary()
	sc2Dir := ""
	if primary != nil {
		sc2Dir = filepath.Join(primary.RootPath, "standalone_games", "wine_games", "StarCraft II")
	}

	args, err := sc2.BuildMatchArgs(opt, sc2Dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if opt.Launch && primary != nil {
		logsDir := filepath.Join(os.TempDir(), "omnihub_logs")
		launched, err := games.LaunchGame(req.Context(), games.LaunchOptions{
			GameDir: sc2Dir,
			ExePath: filepath.Join("Support64", "SC2Switcher_x64.exe"),
			Args:    args,
			LogsDir: logsDir,
			IsAdmin: true,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("launch failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "launched",
			"pid":    launched.PID,
			"args":   args,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ready",
		"args":   args,
	})
}

// --- MEGA 客户端接口 (第 10.2 节) ---

func (r *Router) handleMegaStatus(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	if user.Role != auth.RoleAdmin || auth.IsWANFromContext(req.Context()) {
		http.Error(w, "forbidden: local deck admin required", http.StatusForbidden)
		return
	}
	quota, tasks := r.megaClient.GetState()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"quota": quota,
		"tasks": tasks,
	})
}

func (r *Router) handleMegaDownload(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	if user.Role != auth.RoleAdmin || auth.IsWANFromContext(req.Context()) {
		http.Error(w, "forbidden: local deck admin required", http.StatusForbidden)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID      string `json:"id"`
		Source  string `json:"source"`
		DestDir string `json:"dest_dir"`
	}
	if err := json.NewDecoder(req.Body).Decode(&p); err != nil || p.Source == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.Contains(p.DestDir, "..") {
		http.Error(w, "invalid dest_dir path", http.StatusBadRequest)
		return
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("mega_%d", time.Now().UnixNano())
	}
	if p.DestDir == "" {
		home, _ := os.UserHomeDir()
		p.DestDir = filepath.Join(home, "Downloads")
	}

	task, err := r.megaClient.EnqueueDownload(p.ID, p.Source, p.DestDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (r *Router) checkSectionAccess(w http.ResponseWriter, req *http.Request, sectionID string, target auth.TargetFilter) bool {
	user := auth.UserFromContext(req.Context())
	clientTarget := auth.ClientFromContext(req.Context())
	reqLvl, err := r.authDB.GetSectionRequiredLevel(sectionID)
	if err != nil {
		http.Error(w, "permission lookup failed", http.StatusServiceUnavailable)
		return false
	}
	if !auth.CheckAccess(user.Level, reqLvl, target, clientTarget) {
		http.Error(w, "forbidden: insufficient permission level or target mismatch", http.StatusForbidden)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
