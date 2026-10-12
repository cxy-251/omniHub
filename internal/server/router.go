package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/tools"
	"omnihub/web"
)

// Router 负责 HTTP 请求的统一路由分发与鉴权控制。
type Router struct {
	mux       *http.ServeMux
	libMgr    *library.Manager
	authDB    *auth.DB
	authMid   *auth.Middleware
	clipboard *tools.ClipboardManager
}

// NewRouter 创建路由分发器实例。
func NewRouter(libMgr *library.Manager, authDB *auth.DB) *Router {
	r := &Router{
		mux:       http.NewServeMux(),
		libMgr:    libMgr,
		authDB:    authDB,
		authMid:   auth.NewMiddleware(authDB),
		clipboard: tools.NewClipboardManager(50),
	}
	r.registerRoutes()
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 全局应用鉴权中间件
	r.authMid.Handler(r.mux).ServeHTTP(w, req)
}

func (r *Router) registerRoutes() {
	// 基础状态与健康检查
	r.mux.HandleFunc("/api/health", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "ok",
		})
	})

	// 用户系统与状态接口
	r.mux.HandleFunc("/api/auth/status", r.handleAuthStatus)
	r.mux.HandleFunc("/api/auth/login", r.handleAuthLogin)

	// 媒体库管理接口
	r.mux.HandleFunc("/api/library/list", r.handleLibraryList)

	// 局域网小工具接口
	r.mux.HandleFunc("/api/tools/clipboard", r.handleToolsClipboard)
	r.mux.HandleFunc("/api/tools/metrics", r.handleToolsMetrics)
	r.mux.HandleFunc("/api/tools/wol", r.handleToolsWOL)

	// 嵌入式 Web 根路由静态文件
	r.mux.Handle("/", http.FileServer(http.FS(web.FS)))
}

func (r *Router) handleAuthStatus(w http.ResponseWriter, req *http.Request) {
	user := auth.UserFromContext(req.Context())
	hasAdmin, _ := r.authDB.HasAdmin()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logged_in": user.ID > 0,
		"user":      user,
		"has_admin": hasAdmin,
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

	token := fmt.Sprintf("token_%d_%d", user.ID, user.Level)
	_ = r.authDB.CreateSession(user.ID, token, 86400*1000*1000*1000)

	http.SetCookie(w, &http.Cookie{
		Name:     "omni_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
