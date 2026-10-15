package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/mega"
)

// ConfigureMega 设置 MEGAcmd 安装目录（空表示用 PATH 里的 mega-exec）。
func (r *Router) ConfigureMega(dir string) {
	if r.megaClient != nil {
		r.megaClient.Close()
	}
	r.megaClient = mega.NewClient(dir, 5*time.Minute)
}

// handleMega 是 MEGA 网盘接口（规格 10.2）。账号与网盘内容是隐私：只允许本机受信任客户端的管理员
// （本机限制在 access.go 统一检查）。所有命令都经 mega-exec 执行，服务按需启动、空闲关闭。
func (r *Router) handleMega(w http.ResponseWriter, req *http.Request) {
	if auth.UserFromContext(req.Context()).Role != auth.RoleAdmin || auth.IsWANFromContext(req.Context()) {
		http.Error(w, "forbidden: local admin required", http.StatusForbidden)
		return
	}
	if r.megaClient == nil || !r.megaClient.Available() {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "error": "没有找到 MEGAcmd（mega-exec）"})
		return
	}
	ctx := req.Context()
	c := r.megaClient
	action := strings.TrimPrefix(req.URL.Path, "/api/mega/")
	if req.Method == http.MethodGet {
		switch action {
		case "status":
			writeJSON(w, http.StatusOK, c.GetStatus(ctx))
		case "files", "trash":
			remote := req.URL.Query().Get("path")
			if action == "trash" {
				remote = "//bin"
			}
			if remote == "" {
				remote = "/"
			}
			if !strings.HasPrefix(remote, "/") || len(remote) > 4096 {
				http.Error(w, "invalid path", http.StatusBadRequest)
				return
			}
			list, err := c.List(ctx, remote)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"path": remote, "items": list})
		case "transfers":
			list, st, err := c.Transfers(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"transfers": list, "over_quota": st.OverQuota, "quota_until": st.QuotaUntil, "quota_remaining_sec": st.QuotaRemain})
		case "destinations":
			writeJSON(w, http.StatusOK, r.megaDestinations())
		default:
			http.NotFound(w, req)
		}
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		AuthCode string `json:"auth_code"`
		Source   string `json:"source"`
		Dest     string `json:"dest"`
		Path     string `json:"path"`
		Tag      string `json:"tag"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	var err error
	switch action {
	case "login":
		if in.Email == "" || in.Password == "" {
			http.Error(w, "邮箱和密码不能为空", http.StatusBadRequest)
			return
		}
		err = c.Login(ctx, strings.TrimSpace(in.Email), in.Password, strings.TrimSpace(in.AuthCode))
	case "logout":
		err = c.Simple(ctx, 30*time.Second, "logout")
	case "reload":
		err = c.Simple(ctx, 60*time.Second, "reload")
	case "download":
		// 来源：云端路径（/ 开头）或 mega.nz 公开链接；保存位置必须是本机已存在父目录的绝对路径。
		src := strings.TrimSpace(in.Source)
		if !(strings.HasPrefix(src, "/") || strings.HasPrefix(src, "https://mega.nz/") || strings.HasPrefix(src, "https://mega.co.nz/")) || len(src) > 4096 {
			http.Error(w, "只支持云端路径或 mega.nz 链接", http.StatusBadRequest)
			return
		}
		dest := filepath.Clean(in.Dest)
		if !filepath.IsAbs(dest) {
			http.Error(w, "保存位置必须是绝对路径", http.StatusBadRequest)
			return
		}
		if info, statErr := os.Stat(filepath.Dir(dest)); statErr != nil || !info.IsDir() {
			http.Error(w, "保存位置的上级目录不存在", http.StatusBadRequest)
			return
		}
		err = c.Download(ctx, src, dest)
	case "transfer":
		if in.Tag != "all" && !tagPattern.MatchString(in.Tag) {
			http.Error(w, "invalid tag", http.StatusBadRequest)
			return
		}
		err = c.ControlTransfer(ctx, in.Tag, in.Action)
	case "trash":
		// 移到网盘回收站（可恢复）；不允许操作根目录与回收站本身。
		if !strings.HasPrefix(in.Path, "/") || strings.HasPrefix(in.Path, "//") || strings.Trim(in.Path, "/") == "" {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		err = c.Simple(ctx, 30*time.Second, "mv", in.Path, "//bin/")
	case "restore":
		if !strings.HasPrefix(in.Path, "//bin/") || strings.Contains(in.Path, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		err = c.Simple(ctx, 30*time.Second, "mv", in.Path, "/")
	default:
		http.NotFound(w, req)
		return
	}
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, mega.ErrUnavailable) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

var tagPattern = regexp.MustCompile(`^[0-9]{1,12}$`)

// megaDestinations 列出常用保存位置：收件箱（~/Downloads）与各可写资源库的下载暂存目录。
func (r *Router) megaDestinations() []map[string]string {
	var out []map[string]string
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, map[string]string{"label": "收件箱", "path": filepath.Join(home, "Downloads")})
	}
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline || lib.ReadOnly {
			continue
		}
		out = append(out, map[string]string{"label": "游戏暂存区 · " + lib.Name, "path": filepath.Join(lib.RootPath, "standalone_games", "_downloads")})
	}
	return out
}
