package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/net/webdav"

	"omnihub/internal/auth"
	"omnihub/internal/platform"
)

// WebDAV：把「共享文件」目录（主库 media_library/shared）挂成网络盘，在 Windows 资源管理器、macOS 访达、
// Dolphin、手机文件管理器里直接读写。地址 http://<Deck>:8998/dav/，用 omniHub 账号登录（HTTP Basic）。
// 权限与网页端上传共享文件相同（工具 tools/shared 的等级），只限局域网。删除的文件进系统回收站。
const davPrefix = "/dav"

type davAuthCache struct {
	mu      sync.Mutex
	entries map[[32]byte]davAuthEntry
}

type davAuthEntry struct {
	user    *auth.User
	expires time.Time
}

// login 校验 Basic 凭证。WebDAV 客户端每个请求都带凭证，逐个 bcrypt 太慢，通过的凭证缓存 5 分钟。
func (c *davAuthCache) login(db *auth.DB, req *http.Request) (*auth.User, error) {
	name, password, ok := req.BasicAuth()
	if !ok {
		return nil, errors.New("credentials required")
	}
	key := sha256.Sum256([]byte(name + "\x00" + password))
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && time.Now().Before(e.expires) {
		c.mu.Unlock()
		return e.user, nil
	}
	c.mu.Unlock()
	user, err := db.Authenticate(req.RemoteAddr, name, password)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.entries == nil || len(c.entries) > 64 {
		c.entries = map[[32]byte]davAuthEntry{}
	}
	c.entries[key] = davAuthEntry{user: user, expires: time.Now().Add(5 * time.Minute)}
	c.mu.Unlock()
	return user, nil
}

func (r *Router) handleWebDAV(w http.ResponseWriter, req *http.Request) {
	if auth.IsWANFromContext(req.Context()) {
		http.Error(w, "forbidden: LAN only", http.StatusForbidden)
		return
	}
	user, err := r.davAuth.login(r.authDB, req)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="omniHub", charset="UTF-8"`)
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, user))
	if !r.toolsAllowed(w, req, "shared", true) {
		return
	}
	dir, err := r.sharedDir()
	if err != nil {
		http.Error(w, "shared files unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	// 服务默认 30 秒读完请求体；网络盘上传大文件按单次请求放宽。
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(6 * time.Hour))
	_ = rc.SetWriteDeadline(time.Now().Add(6 * time.Hour))
	(&webdav.Handler{Prefix: davPrefix, FileSystem: trashDir{webdav.Dir(dir), dir}, LockSystem: r.davLocks}).ServeHTTP(w, req)
}

// trashDir 与 webdav.Dir 相同，只是删除（含 MOVE 覆盖目标）改为移进系统回收站。
type trashDir struct {
	webdav.Dir
	root string
}

func (d trashDir) RemoveAll(_ context.Context, name string) error {
	clean := path.Clean("/" + name)
	if clean == "/" {
		return os.ErrInvalid
	}
	target := filepath.Join(d.root, filepath.FromSlash(clean))
	if _, err := os.Lstat(target); err != nil {
		return err
	}
	return platform.Trash(target)
}
