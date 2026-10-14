// Package auth 实现用户系统、身份凭据与访问权限控制。
// 遵循 ARCHITECTURE.md 第 5 节规范。
package auth

import "time"

// 用户等级常量定义（第 5.1 节）
const (
	LevelGuest = 0
	LevelVIP0  = 1
	LevelVIP1  = 2
	LevelVIP2  = 3
	LevelVIP3  = 4
	LevelAdmin = 99
)

// 角色类型
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// 客户端端目标类型（第 4.2 节、第 5.2 节）
const (
	TargetGUI = "gui" // 本地 Qt 6 原生客户端
	TargetWeb = "web" // 远程 Web 浏览器客户端
)

// User 表示系统的用户实体。
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	AuthVersion  int64     `json:"-"`
	Role         string    `json:"role"`
	Level        int       `json:"level"`
	Status       string    `json:"status"` // "active" 或 "disabled"
	CreatedAt    time.Time `json:"created_at"`
}

// Session 记录用户登录会话。
type Session struct {
	TokenHash string    `json:"token_hash"`
	UserID    int64     `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SectionPermission 记录分类大标签的准入门槛。
type SectionPermission struct {
	SectionID     string `json:"section_id"`
	RequiredLevel int    `json:"required_level"`
}

// TrustedDevice 记录通过审核的局域网信任设备（第 8.1 节）。
type TrustedDevice struct {
	DeviceName string    `json:"device_name"`
	ID         string    `json:"id"`
	UserID     int64     `json:"user_id"`
	Username   string    `json:"username"`
	ExpiresAt  time.Time `json:"expires_at"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}
