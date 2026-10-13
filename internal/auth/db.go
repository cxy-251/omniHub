package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"omnihub/internal/storage"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// DB 封装基于 SQLite 的用户认证与权限持久化。
type DB struct {
	db             *sql.DB
	ipMu           sync.Mutex
	ipFailed       map[string][]time.Time // 防暴力破解：记录单 IP 验证失败时间戳
	hasAdminCached int32                  // 0: 未知/未初始化, 1: 已有管理员
}

// OpenDB 打开或创建指定路径下的 users.db，自动执行建表与 WAL 模式开启。
func OpenDB(dbPath string) (*DB, error) {
	sqliteDB, err := storage.OpenSQLite(dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to open auth database: %w", err)
	}

	sqliteDB.SetMaxOpenConns(2)
	sqliteDB.SetMaxIdleConns(1)
	authDB := &DB{
		db:       sqliteDB,
		ipFailed: make(map[string][]time.Time),
	}

	if err := authDB.migrate(); err != nil {
		_ = sqliteDB.Close()
		return nil, fmt.Errorf("auth db migration failed: %w", err)
	}

	return authDB, nil
}

// Close 关闭底层数据库连接。
func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL,
		level INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS section_permissions (
		section_id TEXT PRIMARY KEY,
		required_level INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS user_sessions (
		token_hash TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expires_at TEXT NOT NULL,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS trusted_devices (
		token_hash TEXT PRIMARY KEY,
		device_name TEXT NOT NULL,
		ip TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	`
	_, err := d.db.Exec(schema)
	return err
}

// HashToken 计算会话 Token 的 SHA-256 哈希值作为存储键。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateSecureToken 生成 32 字节密码学安全的随机会话 Token。
func GenerateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CheckIPRateLimit 检查指定 IP 是否因连续失败触发防爆破锁定（连续 5 次失败锁定 5 分钟）。
func (d *DB) CheckIPRateLimit(ip string) error {
	d.ipMu.Lock()
	defer d.ipMu.Unlock()

	now := time.Now()
	cleanIP := sanitizeIP(ip)
	timestamps := d.ipFailed[cleanIP]

	// 过滤 5 分钟之前的时间戳
	valid := make([]time.Time, 0, len(timestamps))
	for _, t := range timestamps {
		if now.Sub(t) < 5*time.Minute {
			valid = append(valid, t)
		}
	}
	d.ipFailed[cleanIP] = valid

	if len(valid) >= 5 {
		waitSec := int((5 * time.Minute) - now.Sub(valid[len(valid)-1]))
		return fmt.Errorf("ip temporarily locked due to repeated failures, retry in %d seconds", waitSec)
	}
	return nil
}

// RecordIPFailure 记录一次特定 IP 的鉴权失败。
func (d *DB) RecordIPFailure(ip string) {
	d.ipMu.Lock()
	defer d.ipMu.Unlock()

	cleanIP := sanitizeIP(ip)
	d.ipFailed[cleanIP] = append(d.ipFailed[cleanIP], time.Now())
}

// ClearIPFailure 鉴权成功后清除该 IP 的失败记录。
func (d *DB) ClearIPFailure(ip string) {
	d.ipMu.Lock()
	defer d.ipMu.Unlock()

	delete(d.ipFailed, sanitizeIP(ip))
}

func sanitizeIP(ip string) string {
	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		return host
	}
	return ip
}

// HasAdmin 检查系统是否已有管理员账号（用于首次启动引导判断）。
func (d *DB) HasAdmin() (bool, error) {
	if atomic.LoadInt32(&d.hasAdminCached) == 1 {
		return true, nil
	}
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'").Scan(&count)
	if err == nil && count > 0 {
		atomic.StoreInt32(&d.hasAdminCached, 1)
		return true, nil
	}
	return count > 0, err
}

// GetActiveAdmin 返回实际管理员账号，供可信本地客户端使用，避免假定管理员 ID 为 1。
func (d *DB) GetActiveAdmin() (*User, error) {
	var u User
	var created string
	err := d.db.QueryRow(`SELECT id, username, role, level, status, created_at FROM users
		WHERE role = 'admin' AND status = 'active' ORDER BY id LIMIT 1`).Scan(
		&u.ID, &u.Username, &u.Role, &u.Level, &u.Status, &created)
	if err != nil {
		return nil, fmt.Errorf("query active administrator: %w", err)
	}
	u.CreatedAt, err = time.Parse(time.RFC3339, created)
	if err != nil {
		return nil, fmt.Errorf("invalid administrator timestamp: %w", err)
	}
	return &u, nil
}

// CreateUser 创建新用户并对明文密码进行 bcrypt 哈希处理。
func (d *DB) CreateUser(username, password, role string, level int) (*User, error) {
	if username == "" || len(username) > 64 || password == "" || len(password) > 72 {
		return nil, errors.New("username and password cannot be empty")
	}
	if (role != RoleAdmin && role != RoleUser) || !validLevel(level) || (role == RoleAdmin && level != LevelAdmin) {
		return nil, errors.New("invalid user role or level")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	res, err := d.db.Exec(`
		INSERT INTO users (username, password_hash, role, level, status, created_at)
		VALUES (?, ?, ?, ?, 'active', ?)`,
		username, string(hash), role, level, nowStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert user: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if role == RoleAdmin {
		atomic.StoreInt32(&d.hasAdminCached, 1)
	}

	return &User{
		ID:           id,
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		Level:        level,
		Status:       "active",
		CreatedAt:    now,
	}, nil
}

func validLevel(level int) bool {
	return level >= LevelGuest && level <= LevelVIP3 || level == LevelAdmin
}

// CreateInitialAdmin 原子检查并创建首次管理员，防止并发初始化生成多个管理员。
func (d *DB) CreateInitialAdmin(username, password string) (*User, error) {
	if username == "" || len(username) > 64 || password == "" || len(password) > 72 {
		return nil, errors.New("invalid administrator credentials")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	tx, err := d.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE role='admin'").Scan(&count); err != nil {
		return nil, err
	}
	if count != 0 {
		return nil, errors.New("administrator already exists")
	}
	now := time.Now().UTC()
	result, err := tx.Exec(`INSERT INTO users(username,password_hash,role,level,status,created_at) VALUES (?,?, 'admin',99,'active',?)`, username, string(hash), now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	atomic.StoreInt32(&d.hasAdminCached, 1)
	return &User{ID: id, Username: username, Role: RoleAdmin, Level: LevelAdmin, Status: "active", CreatedAt: now}, nil
}

// Authenticate 校验用户名与密码，支持 IP 速率限制保护。
func (d *DB) Authenticate(ip, username, password string) (*User, error) {
	if err := d.CheckIPRateLimit(ip); err != nil {
		return nil, err
	}

	var u User
	var createdAtStr string
	err := d.db.QueryRow(`
		SELECT id, username, password_hash, role, level, status, created_at
		FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Level, &u.Status, &createdAtStr)

	if err != nil {
		d.RecordIPFailure(ip)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid username or password")
		}
		return nil, err
	}

	if u.Status != "active" {
		return nil, errors.New("user account is disabled")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		d.RecordIPFailure(ip)
		return nil, errors.New("invalid username or password")
	}

	d.ClearIPFailure(ip)
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	return &u, nil
}

// CreateSession 创建并保存登录 Token 会话。
func (d *DB) CreateSession(userID int64, token string, duration time.Duration) error {
	tokenHash := HashToken(token)
	expiresAt := time.Now().UTC().Add(duration).Format(time.RFC3339)

	_, err := d.db.Exec(`
		INSERT INTO user_sessions (token_hash, user_id, expires_at)
		VALUES (?, ?, ?)`,
		tokenHash, userID, expiresAt,
	)
	return err
}

// GetUserByToken 根据 Token 查询对应用户信息并校验过期状态。
func (d *DB) GetUserByToken(token string) (*User, error) {
	tokenHash := HashToken(token)
	nowStr := time.Now().UTC().Format(time.RFC3339)

	var u User
	var createdAtStr string
	err := d.db.QueryRow(`
		SELECT u.id, u.username, u.role, u.level, u.status, u.created_at
		FROM user_sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.status = 'active'`,
		tokenHash, nowStr,
	).Scan(&u.ID, &u.Username, &u.Role, &u.Level, &u.Status, &createdAtStr)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // 无效或过期 Token
		}
		return nil, err
	}

	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	return &u, nil
}

// RevokeUserSessions 废止指定用户的所有活跃会话（用于降权或修改等级时即时生效，第 5.3.2 节）。
func (d *DB) RevokeUserSessions(userID int64) error {
	_, err := d.db.Exec("DELETE FROM user_sessions WHERE user_id = ?", userID)
	return err
}

// SetSectionRequiredLevel 设置分类标签的最低准入等级。
func (d *DB) SetSectionRequiredLevel(sectionID string, requiredLevel int) error {
	if sectionID == "" || len(sectionID) > 64 || !validLevel(requiredLevel) {
		return errors.New("invalid section permission")
	}
	_, err := d.db.Exec(`
		INSERT INTO section_permissions (section_id, required_level)
		VALUES (?, ?)
		ON CONFLICT(section_id) DO UPDATE SET required_level = excluded.required_level`,
		sectionID, requiredLevel,
	)
	return err
}

// GetSectionRequiredLevel 获取指定分类的门槛等级（缺省默认 0）。
func (d *DB) GetSectionRequiredLevel(sectionID string) (int, error) {
	var level int
	err := d.db.QueryRow("SELECT required_level FROM section_permissions WHERE section_id = ?", sectionID).Scan(&level)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return level, nil
}

// ListSectionPermissions 列出所有分类权限矩阵。
func (d *DB) ListSectionPermissions() (map[string]int, error) {
	rows, err := d.db.Query("SELECT section_id, required_level FROM section_permissions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]int)
	for rows.Next() {
		var sec string
		var lvl int
		if err := rows.Scan(&sec, &lvl); err != nil {
			return nil, err
		}
		res[sec] = lvl
	}
	return res, rows.Err()
}

// ListUsers 列出所有用户。
func (d *DB) ListUsers() ([]*User, error) {
	rows, err := d.db.Query("SELECT id, username, role, level, status, created_at FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var createdStr string
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Level, &u.Status, &createdStr); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		users = append(users, &u)
	}
	return users, rows.Err()
}

// UpdateUserLevel 修改用户等级并废止所有活跃会话以即时生效。
func (d *DB) UpdateUserLevel(userID int64, level int) error {
	if userID <= 0 || !validLevel(level) {
		return errors.New("invalid user or level")
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role string
	if err := tx.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role); err != nil {
		return err
	}
	if role == RoleAdmin && level != LevelAdmin {
		return errors.New("administrator level must remain 99")
	}
	if _, err := tx.Exec("UPDATE users SET level=? WHERE id=?", level, userID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM user_sessions WHERE user_id=?", userID); err != nil {
		return err
	}
	return tx.Commit()
}
