package auth

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type contextKey string

const (
	UserContextKey   contextKey = "auth_user"
	ClientContextKey contextKey = "client_target"
)

// Middleware 负责在 HTTP 链路中提取会话凭证并注入用户信息。
type Middleware struct {
	db *DB
}

// NewMiddleware 创建中间件实例。
func NewMiddleware(db *DB) *Middleware {
	return &Middleware{db: db}
}

// isLoopback 判断请求是否来自本地回环（第 8.1.1 节受信任环境）。
func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host == "localhost"
	}
	return ip.IsLoopback()
}

// Handler 返回 HTTP 鉴权处理中间件。
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 判定客户端类型
		clientTarget := TargetWeb
		if r.Header.Get("X-Client-Target") == TargetGUI {
			clientTarget = TargetGUI
		}
		ctx = context.WithValue(ctx, ClientContextKey, clientTarget)

		// 检查系统是否有 Admin（第 5.3.3 节）
		hasAdmin, _ := m.db.HasAdmin()
		if !hasAdmin && !isLoopback(r.RemoteAddr) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"system uninitialized, admin setup required on local host"}`))
			return
		}

		// 1. 本地回环免密直通 Admin（第 8.1.1 节）
		if isLoopback(r.RemoteAddr) {
			adminUser := &User{
				ID:       1,
				Username: "local_admin",
				Role:     RoleAdmin,
				Level:    LevelAdmin,
				Status:   "active",
			}
			ctx = context.WithValue(ctx, UserContextKey, adminUser)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 2. 提取 Token (Header Bearer 或 Cookie)
		token := ""
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else if cookie, err := r.Cookie("omni_token"); err == nil {
			token = cookie.Value
		}

		if token != "" {
			user, err := m.db.GetUserByToken(token)
			if err == nil && user != nil {
				ctx = context.WithValue(ctx, UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 3. 未登录默认降级为 Guest (Level 0)
		guestUser := &User{
			ID:       0,
			Username: "guest",
			Role:     RoleUser,
			Level:    LevelGuest,
			Status:   "active",
		}
		ctx = context.WithValue(ctx, UserContextKey, guestUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext 从上下文中获取当前请求的用户信息。
func UserFromContext(ctx context.Context) *User {
	if u, ok := ctx.Value(UserContextKey).(*User); ok && u != nil {
		return u
	}
	return &User{Level: LevelGuest}
}

// ClientFromContext 从上下文中获取当前请求的端目标类型。
func ClientFromContext(ctx context.Context) string {
	if c, ok := ctx.Value(ClientContextKey).(string); ok && c != "" {
		return c
	}
	return TargetWeb
}
