package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// DB 封装基于 SQLite 的用户认证与权限持久化。
type DB struct {
	db       *sql.DB
	ipMu     sync.Mutex
	ipFailed map[string][]time.Time // 防暴力破解：记录单 IP 验证失败时间戳
}

// OpenDB 打开或创建指定路径下的 users.db，自动执行建表与 WAL 模式开启。
func OpenDB(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create auth db directory: %w", err)
	}

	// 开启 WAL 模式以支撑并发读写
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	sqliteDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open auth database: %w", err)
	}

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
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'").Scan(&count)
	return count > 0, err
}

// CreateUser 创建新用户并对明文密码进行 bcrypt 哈希处理。
func (d *DB) CreateUser(username, password, role string, level int) (*User, error) {
	if username == "" || password == "" {
		return nil, errors.New("username and password cannot be empty")
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
