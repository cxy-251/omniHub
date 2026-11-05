package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"
)

// DefaultSessionIdleDays applies until an administrator saves a different limit.
const DefaultSessionIdleDays = 30

// MaxSessionIdleDays bounds persistent browser sessions to one year.
const MaxSessionIdleDays = 365

// SessionIdleDays returns the persisted password-session inactivity limit.
func (d *DB) SessionIdleDays() int {
	d.sessionMu.RLock()
	defer d.sessionMu.RUnlock()
	return d.sessionDays
}

// SessionIdleTimeout returns the currently configured password-session lifetime.
func (d *DB) SessionIdleTimeout() time.Duration {
	return time.Duration(d.SessionIdleDays()) * 24 * time.Hour
}

// SetSessionIdleDays saves a limit for new logins and subsequent renewals.
// Existing expired or revoked sessions are never restored by this change.
func (d *DB) SetSessionIdleDays(ctx context.Context, days int) error {
	if days < 1 || days > MaxSessionIdleDays {
		return ErrAccountInput
	}
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if _, err := d.db.ExecContext(ctx, `INSERT INTO auth_settings(id,session_idle_days) VALUES(1,?)
		ON CONFLICT(id) DO UPDATE SET session_idle_days=excluded.session_idle_days`, days); err != nil {
		return err
	}
	d.sessionDays = days
	return nil
}

// Hourly renewal avoids a database write and Set-Cookie for every media request.
const sessionRenewInterval = time.Hour

func (d *DB) session(ctx context.Context, hash string, now time.Time) (*User, time.Time, error) {
	if len(hash) != 64 {
		return nil, time.Time{}, errors.New("invalid session hash")
	}
	var user User
	var createdAt, expiresAt string
	err := d.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, u.level, u.status, u.created_at, s.expires_at
		FROM user_sessions s JOIN users u ON s.user_id = u.id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.status = 'active'`,
		hash, now.Format(time.RFC3339),
	).Scan(&user.ID, &user.Username, &user.Role, &user.Level, &user.Status, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return nil, time.Time{}, err
	}
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &user, expires, nil
}

// UseSession authenticates an active request and periodically extends its idle
// deadline. A nonzero expiry means the browser cookie must also be renewed.
// Passive resource-grant checks use GetUserBySessionHash and never extend it.
func (d *DB) UseSession(ctx context.Context, hash string) (*User, time.Time, error) {
	now := time.Now().UTC()
	timeout := d.SessionIdleTimeout()
	user, expires, err := d.session(ctx, hash, now)
	remaining := expires.Sub(now)
	if err != nil || user == nil || (remaining > timeout-sessionRenewInterval && remaining <= timeout) {
		return user, time.Time{}, err
	}
	deadline := now.Add(timeout).Truncate(time.Second)
	result, err := d.db.ExecContext(ctx, `
		UPDATE user_sessions SET expires_at = ?
		WHERE token_hash = ? AND expires_at = ? AND expires_at > ?
		AND EXISTS (SELECT 1 FROM users WHERE id = user_sessions.user_id AND status = 'active')`,
		deadline.Format(time.RFC3339), hash, expires.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, time.Time{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, time.Time{}, err
	}
	if count == 0 {
		// Another request may have renewed or revoked it. Never insert a session
		// here: logout, password resets and account disable must remain final.
		return d.session(ctx, hash, time.Now().UTC())
	}
	return user, deadline, nil
}

// SetSessionCookie persists a password login until its database deadline.
func SetSessionCookie(w http.ResponseWriter, req *http.Request, token string, expires time.Time) {
	maxAge := int((time.Until(expires) + time.Second - 1) / time.Second)
	if maxAge <= 0 {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{
		Name: "omni_token", Value: token, Path: "/", HttpOnly: true,
		Secure: req.TLS != nil, SameSite: http.SameSiteStrictMode,
		MaxAge: maxAge, Expires: expires,
	})
}
