package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ErrAccountMissing indicates a target ID that no longer exists.
var ErrAccountMissing = errors.New("account not found")

// ErrAccountExists identifies a duplicate username without masking storage failures.
var ErrAccountExists = errors.New("username already exists")

// ErrLastAdmin protects local initialization and recovery from administrative lockout.
var ErrLastAdmin = errors.New("cannot disable the last active administrator")

// ErrAccountInput reports invalid account fields before changing credentials.
var ErrAccountInput = errors.New("invalid account input")

// ErrCredentialChanged requires repeating password verification after a concurrent change.
var ErrCredentialChanged = errors.New("account credentials changed; sign in again")

// ListUsersPage provides bounded account administration without password hashes.
func (d *DB) ListUsersPage(ctx context.Context, limit, offset int) (users []*User, result error) {
	if limit < 1 || limit > 101 || offset < 0 || offset > 1000000 {
		return nil, ErrAccountInput
	}
	rows, err := d.db.QueryContext(ctx, "SELECT id,username,role,level,status,created_at FROM users ORDER BY id LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, rows.Close()) }()
	users = []*User{}
	for rows.Next() {
		var user User
		var created string
		if err = rows.Scan(&user.ID, &user.Username, &user.Role, &user.Level, &user.Status, &created); err != nil {
			return nil, err
		}
		user.CreatedAt, err = time.Parse(time.RFC3339, created)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}
	return users, rows.Err()
}

func revokeAccount(ctx context.Context, tx *sql.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE user_id=?", id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM trusted_devices WHERE user_id=?", id)
	return err
}

// SetUserStatus changes one account and revokes all credentials in the same
// transaction. A single SQL predicate serializes the last-admin invariant.
func (d *DB) SetUserStatus(ctx context.Context, id int64, status string) (result error) {
	if id <= 0 || (status != "active" && status != "disabled") {
		return ErrAccountInput
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, err)
		}
	}()
	changed, err := tx.ExecContext(ctx, `UPDATE users SET status=?,auth_version=auth_version+1 WHERE id=? AND status!=? AND (?='active' OR role!='admin' OR status!='active' OR (SELECT COUNT(*) FROM users WHERE role='admin' AND status='active')>1)`, status, id, status, status)
	if err != nil {
		return err
	}
	count, err := changed.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var current string
		err = tx.QueryRowContext(ctx, "SELECT status FROM users WHERE id=?", id).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAccountMissing
		}
		if err != nil {
			return err
		}
		if current != status {
			return ErrLastAdmin
		}
		return tx.Commit()
	}
	if err = revokeAccount(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPassword hashes a replacement password and atomically invalidates normal
// and remembered credentials. Existing media, history and account IDs survive.
func (d *DB) SetPassword(ctx context.Context, id int64, password string) error {
	return d.setPassword(ctx, id, password, nil)
}

// SetVerifiedPassword protects self-service changes against concurrent resets.
func (d *DB) SetVerifiedPassword(ctx context.Context, user *User, password string) error {
	if user == nil || user.PasswordHash == "" {
		return ErrAccountInput
	}
	return d.setPassword(ctx, user.ID, password, user)
}

func (d *DB) setPassword(ctx context.Context, id int64, password string, proof *User) (result error) {
	if id <= 0 || password == "" || len(password) > 72 {
		return ErrAccountInput
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, err)
		}
	}()
	query := "UPDATE users SET password_hash=?,auth_version=auth_version+1 WHERE id=?"
	args := []any{string(hash), id}
	if proof != nil {
		query += " AND password_hash=? AND auth_version=? AND status='active'"
		args = append(args, proof.PasswordHash, proof.AuthVersion)
	}
	updated, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		if proof != nil {
			return ErrCredentialChanged
		}
		return ErrAccountMissing
	}
	if err = revokeAccount(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateVerifiedSession closes the verification/reset race: a password checked
// against an older hash cannot create a fresh session after reset or disable.
func (d *DB) CreateVerifiedSession(ctx context.Context, user *User, token string, duration time.Duration) error {
	if user == nil || user.ID <= 0 || user.PasswordHash == "" || token == "" || duration <= 0 || duration > 24*time.Hour {
		return ErrAccountInput
	}
	added, err := d.db.ExecContext(ctx, `INSERT INTO user_sessions(token_hash,user_id,expires_at) SELECT ?,id,? FROM users WHERE id=? AND password_hash=? AND auth_version=? AND status='active'`, HashToken(token), time.Now().UTC().Add(duration).Format(time.RFC3339), user.ID, user.PasswordHash, user.AuthVersion)
	if err != nil {
		return err
	}
	count, err := added.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrCredentialChanged
	}
	return nil
}

func (d *DB) migrateAccountVersion() error {
	rows, err := d.db.Query("PRAGMA table_info(users)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var index, notNull, primary int
		var name, kind string
		var value any
		if err = rows.Scan(&index, &name, &kind, &notNull, &value, &primary); err != nil {
			break
		}
		if name == "auth_version" {
			found = true
		}
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = d.db.Exec("ALTER TABLE users ADD COLUMN auth_version INTEGER NOT NULL DEFAULT 0")
	return err
}
