package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAccessChecker(t *testing.T) {
	// 准入公式：允许访问 = (用户当前等级 >= 分类门槛) ∧ (端目标匹配)

	// 测试用例 1: 等级不足
	if CheckAccess(LevelGuest, LevelVIP1, TargetAll, TargetWeb) {
		t.Error("expected access denied for lower level")
	}

	// 测试用例 2: 等级足够，双端公开
	if !CheckAccess(LevelVIP1, LevelVIP1, TargetAll, TargetWeb) {
		t.Error("expected access granted for equal level")
	}

	// 测试用例 3: gui_only 分类，在 web 客户端访问应被拦截
	if CheckAccess(LevelAdmin, LevelGuest, TargetGUIOnly, TargetWeb) {
		t.Error("expected access denied for gui_only on web client")
	}

	// 测试用例 4: gui_only 分类，在 gui 客户端访问应通过
	if !CheckAccess(LevelAdmin, LevelGuest, TargetGUIOnly, TargetGUI) {
		t.Error("expected access granted for gui_only on gui client")
	}

	// 测试用例 5: web_only 分类，在 gui 客户端访问应被拦截
	if CheckAccess(LevelAdmin, LevelGuest, TargetWebOnly, TargetGUI) {
		t.Error("expected access denied for web_only on gui client")
	}
}

func TestAuthDBLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "users.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open auth db: %v", err)
	}
	defer db.Close()

	// 初始无管理员
	hasAdmin, err := db.HasAdmin()
	if err != nil || hasAdmin {
		t.Fatalf("expected no admin initially, got %v", hasAdmin)
	}

	// 创建管理员用户
	admin, err := db.CreateUser("admin", "secret123", RoleAdmin, LevelAdmin)
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}
	if admin.Role != RoleAdmin || admin.Level != LevelAdmin {
		t.Errorf("unexpected admin fields: %+v", admin)
	}

	// 创建普通用户
	user, err := db.CreateUser("testuser", "pass456", RoleUser, LevelVIP1)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// 验证正确账密
	authed, err := db.Authenticate("192.168.1.100", "testuser", "pass456")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}
	if authed.ID != user.ID {
		t.Errorf("expected user id %d, got %d", user.ID, authed.ID)
	}

	// 验证错误密码
	_, err = db.Authenticate("192.168.1.100", "testuser", "wrongpassword")
	if err == nil {
		t.Error("expected error for wrong password")
	}

	// 会话测试
	token := "token_abc_123"
	if err := db.CreateSession(user.ID, token, 1*time.Hour); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	sessionUser, err := db.GetUserByToken(token)
	if err != nil || sessionUser == nil {
		t.Fatalf("expected valid session user, got err: %v", err)
	}
	if sessionUser.ID != user.ID {
		t.Errorf("expected session user id %d, got %d", user.ID, sessionUser.ID)
	}

	// 会话吊销测试（降权时即时生效）
	if err := db.RevokeUserSessions(user.ID); err != nil {
		t.Fatalf("failed to revoke sessions: %v", err)
	}
	revokedUser, err := db.GetUserByToken(token)
	if err != nil || revokedUser != nil {
		t.Errorf("expected nil user after revocation, got: %v", revokedUser)
	}
}

func TestIPRateLimiter(t *testing.T) {
	tempDir := t.TempDir()
	db, err := OpenDB(filepath.Join(tempDir, "users.db"))
	if err != nil {
		t.Fatalf("failed to open auth db: %v", err)
	}
	defer db.Close()

	ip := "192.168.1.50"
	// 模拟连续 5 次失败
	for i := 0; i < 5; i++ {
		db.RecordIPFailure(ip)
	}

	// 第 6 次应被速率限制拦截
	if err := db.CheckIPRateLimit(ip); err == nil {
		t.Error("expected rate limit error after 5 failures")
	}

	// 成功清除后应恢复
	db.ClearIPFailure(ip)
	if err := db.CheckIPRateLimit(ip); err != nil {
		t.Errorf("expected rate limit cleared, got: %v", err)
	}
}
