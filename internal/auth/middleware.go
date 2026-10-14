package auth

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

type contextKey string

const (
	UserContextKey       contextKey = "auth_user"
	ClientContextKey     contextKey = "client_target"
	WANContextKey        contextKey = "is_wan"
	LocalContextKey      contextKey = "trusted_local"
	methodContextKey     contextKey = "authentication_method"
	credentialContextKey contextKey = "credential_hash"
	deviceContextKey     contextKey = "device_id"
)

// Middleware 负责在 HTTP 链路中提取会话凭证并注入用户信息。
type Middleware struct {
	db         *DB
	localToken string
}

// NewMiddleware 创建中间件实例。
func NewMiddleware(db *DB, localToken string) *Middleware {
	return &Middleware{db: db, localToken: localToken}
}

// IsTrustedLocal 校验由服务生成、仅供本机客户端读取的凭证；回环地址本身不授予权限。
func (m *Middleware) IsTrustedLocal(r *http.Request) bool {
	return IsLoopbackDirect(r) && m.localToken != "" &&
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Omni-Local-Token")), []byte(m.localToken)) == 1
}

// IsLoopbackDirect 判断请求是否为直接本地回环访问（排除 Cloudflare 隧道或外部反代伪装）。
func IsLoopbackDirect(r *http.Request) bool {
	if hasForwardingHeaders(r) {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host == "localhost"
	}
	return ip.IsLoopback()
}

// IsWANRequest 判断请求是否来自 WAN 公网或反代隧道（第 8.1.3 节）。
func IsWANRequest(r *http.Request) bool {
	if r.Header.Get("CF-Connecting-IP") != "" ||
		r.Header.Get("X-Forwarded-For") != "" ||
		r.Header.Get("X-Real-IP") != "" {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host != "localhost"
	}
	return !ip.IsLoopback() && !ip.IsPrivate()
}

// Handler 返回 HTTP 鉴权处理中间件。
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/auth/") || strings.HasPrefix(r.URL.Path, "/api/admin/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		ctx := context.WithValue(r.Context(), methodContextKey, "guest")

		// 判定客户端类型与公网来源
		clientTarget := TargetWeb
		isTrustedLocal := m.IsTrustedLocal(r)
		if isTrustedLocal {
			clientTarget = TargetGUI
		}
		isWAN := IsWANRequest(r)

		ctx = context.WithValue(ctx, ClientContextKey, clientTarget)
		ctx = context.WithValue(ctx, WANContextKey, isWAN)
		ctx = context.WithValue(ctx, LocalContextKey, isTrustedLocal)

		// 检查系统是否有 Admin（第 5.3.3 节）
		hasAdmin, err := m.db.HasAdmin()
		if err != nil {
			http.Error(w, "authentication store unavailable", http.StatusServiceUnavailable)
			return
		}
		if !hasAdmin && !isTrustedLocal {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"system uninitialized, admin setup required on local host"}`))
			return
		}

		// 1. 本地回环免密直通 Admin（第 8.1.1 节），未初始化时不直通以引导 setup
		if isTrustedLocal && hasAdmin {
			adminUser, err := m.db.GetActiveAdmin()
			if err != nil {
				http.Error(w, "local administrator unavailable", http.StatusServiceUnavailable)
				return
			}
			ctx = context.WithValue(ctx, methodContextKey, "local")
			ctx = context.WithValue(ctx, UserContextKey, adminUser)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// A presented device cookie owns authentication. If revoked, expired or
		// outside direct RFC1918 LAN, an older normal session cannot rescue it.
		deviceCookie, deviceCookieErr := r.Cookie(DeviceCookie)
		if deviceCookieErr == nil {
			ctx = context.WithValue(ctx, methodContextKey, "device-invalid")
			if IsDirectLAN(r) && validDeviceToken(deviceCookie.Value) {
				hash := HashToken(deviceCookie.Value)
				user, id, err := m.db.TrustedUserByHash(r.Context(), hash)
				if err != nil {
					http.Error(w, "device authentication store unavailable", 503)
					return
				}
				if user != nil {
					ctx = context.WithValue(ctx, UserContextKey, user)
					ctx = context.WithValue(ctx, methodContextKey, "device")
					ctx = context.WithValue(ctx, credentialContextKey, hash)
					ctx = context.WithValue(ctx, deviceContextKey, id)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		} else if hash := SessionHash(r); hash != "" {
			user, err := m.db.GetUserBySessionHash(hash)
			if err != nil {
				http.Error(w, "session store unavailable", 503)
				return
			}
			if user != nil {
				ctx = context.WithValue(ctx, UserContextKey, user)
				ctx = context.WithValue(ctx, methodContextKey, "session")
				ctx = context.WithValue(ctx, credentialContextKey, hash)
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

// IsTrustedLocalFromContext 返回请求是否通过本机专用凭证验证。
func IsTrustedLocalFromContext(ctx context.Context) bool {
	trusted, _ := ctx.Value(LocalContextKey).(bool)
	return trusted
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

// IsWANFromContext 从上下文中获取是否属于公网/隧道请求。
func IsWANFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(WANContextKey).(bool); ok {
		return v
	}
	return false
}

// SessionHash extracts only a bounded password-session proof, never device cookies.
func SessionHash(req *http.Request) string {
	token := ""
	if value := req.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
		token = strings.TrimPrefix(value, "Bearer ")
	} else if cookie, err := req.Cookie("omni_token"); err == nil {
		token = cookie.Value
	}
	if token == "" || len(token) > 4096 {
		return ""
	}
	return HashToken(token)
}

// AuthenticationMethod distinguishes local, password-session and remembered LAN identities.
func AuthenticationMethod(ctx context.Context) string {
	value, _ := ctx.Value(methodContextKey).(string)
	return value
}

// CredentialHash returns the already validated credential identity for scoped resource grants.
func CredentialHash(ctx context.Context) string {
	value, _ := ctx.Value(credentialContextKey).(string)
	return value
}

// DeviceID returns safe current-device metadata, never its secret token.
func DeviceID(ctx context.Context) string {
	value, _ := ctx.Value(deviceContextKey).(string)
	return value
}
