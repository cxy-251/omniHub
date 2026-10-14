package auth

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"
	"unicode"
)

// DeviceCookie names the host-only HttpOnly browser credential.
const DeviceCookie = "omni_device"

// DeviceLifetime bounds remembered browser access without renewal writes.
const DeviceLifetime = 180 * 24 * time.Hour

// MaxTrustedDevices bounds persisted active/expired device metadata.
const MaxTrustedDevices = 64

// ErrInvalidDevice reports invalid registration input.
var ErrInvalidDevice = errors.New("invalid device registration")

// ErrDeviceSession requires a live password session owned by an administrator.
var ErrDeviceSession = errors.New("password session expired or revoked")

// ErrDeviceCapacity requires revoking an existing device before enrollment.
var ErrDeviceCapacity = errors.New("trusted device capacity reached; revoke an existing device")

var lanPrefixes = [...]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16")}

// IsDirectLAN accepts only direct RFC1918 IPv4 peers, including mapped IPv4.
// Forwarding metadata never turns a public/proxied peer into a trusted LAN peer.
func IsDirectLAN(req *http.Request) bool {
	if hasForwardingHeaders(req) {
		return false
	}
	address, err := netip.ParseAddrPort(req.RemoteAddr)
	var ip netip.Addr
	if err == nil {
		ip = address.Addr()
	} else {
		ip, err = netip.ParseAddr(req.RemoteAddr)
		if err != nil {
			return false
		}
	}
	ip = ip.Unmap()
	for _, prefix := range lanPrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func hasForwardingHeaders(req *http.Request) bool {
	for _, name := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP", "Forwarded"} {
		if _, present := req.Header[http.CanonicalHeaderKey(name)]; present {
			return true
		}
	}
	return false
}

func validDeviceID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func validDeviceToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func (d *DB) migrateDevices() (result error) {
	rows, err := d.db.Query("PRAGMA table_info(trusted_devices)")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var index, notNull, primary int
		var name, kind string
		var value any
		if err = rows.Scan(&index, &name, &kind, &notNull, &value, &primary); err != nil {
			break
		}
		columns[name] = true
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return err
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, err)
		}
	}()
	for _, column := range []struct{ name, sql string }{{"device_id", "device_id TEXT NOT NULL DEFAULT ''"}, {"user_id", "user_id INTEGER REFERENCES users(id) ON DELETE CASCADE"}, {"expires_at", "expires_at TEXT NOT NULL DEFAULT ''"}} {
		if !columns[column.name] {
			if _, err = tx.Exec("ALTER TABLE trusted_devices ADD COLUMN " + column.sql); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS trusted_device_ids ON trusted_devices(device_id) WHERE device_id!=''; CREATE INDEX IF NOT EXISTS trusted_device_expiry ON trusted_devices(expires_at) WHERE device_id!=''`); err != nil {
		return err
	}
	return tx.Commit()
}

// RegisterDevice converts one still-valid password session to a bounded LAN
// credential. Only its SHA-256 digest is stored; legacy unowned rows stay inert.
func (d *DB) RegisterDevice(ctx context.Context, userID int64, sessionHash, name, ip string) (device *TrustedDevice, token string, result error) {
	name = strings.TrimSpace(name)
	if userID <= 0 || len(sessionHash) != 64 || name == "" || len(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
		return nil, "", ErrInvalidDevice
	}
	peer := &http.Request{RemoteAddr: ip, Header: http.Header{}}
	if !IsDirectLAN(peer) {
		return nil, "", ErrInvalidDevice
	}
	token, err := GenerateSecureToken()
	if err != nil {
		return nil, "", err
	}
	id, err := GenerateSecureToken()
	if err != nil {
		return nil, "", err
	}
	id = id[:32]
	now := time.Now().UTC()
	expiry := now.Add(DeviceLifetime)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, err)
		}
	}()
	var owner string
	if err = tx.QueryRowContext(ctx, `SELECT u.username FROM user_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.user_id=? AND s.expires_at>? AND u.role='admin' AND u.status='active'`, sessionHash, userID, now.Format(time.RFC3339)).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrDeviceSession
		}
		return nil, "", err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM trusted_devices WHERE device_id!='' AND expires_at<=?", now.Format(time.RFC3339)); err != nil {
		return nil, "", err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM trusted_devices WHERE device_id!=''").Scan(&count); err != nil {
		return nil, "", err
	}
	if count >= MaxTrustedDevices {
		return nil, "", ErrDeviceCapacity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO trusted_devices(token_hash,device_name,ip,created_at,device_id,user_id,expires_at) VALUES(?,?,?,?,?,?,?)`, HashToken(token), name, ip, now.Format(time.RFC3339), id, userID, expiry.Format(time.RFC3339)); err != nil {
		return nil, "", err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE token_hash=?", sessionHash); err != nil {
		return nil, "", err
	}
	if err = tx.Commit(); err != nil {
		return nil, "", err
	}
	return &TrustedDevice{ID: id, UserID: userID, Username: owner, DeviceName: name, IP: ip, CreatedAt: now, ExpiresAt: expiry}, token, nil
}

// TrustedUserByHash rechecks revocation, expiration and the live account on each
// use. Its caller must enforce direct LAN scope; it never updates access times.
func (d *DB) TrustedUserByHash(ctx context.Context, hash string) (*User, string, error) {
	if len(hash) != 64 {
		return nil, "", nil
	}
	var user User
	var created, id string
	err := d.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.role,u.level,u.status,u.created_at,t.device_id FROM trusted_devices t JOIN users u ON u.id=t.user_id WHERE t.token_hash=? AND length(t.device_id)=32 AND t.expires_at>? AND u.role='admin' AND u.status='active'`, hash, time.Now().UTC().Format(time.RFC3339)).Scan(&user.ID, &user.Username, &user.Role, &user.Level, &user.Status, &created, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	user.CreatedAt, err = time.Parse(time.RFC3339, created)
	if err != nil {
		return nil, "", err
	}
	return &user, id, nil
}

// ListDevices returns bounded safe metadata, never token digests or credentials.
func (d *DB) ListDevices(ctx context.Context) (devices []TrustedDevice, result error) {
	rows, err := d.db.QueryContext(ctx, `SELECT t.device_id,t.user_id,u.username,t.device_name,t.ip,t.created_at,t.expires_at FROM trusted_devices t JOIN users u ON u.id=t.user_id WHERE t.device_id!='' ORDER BY t.created_at DESC,t.device_id LIMIT 65`)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, rows.Close()) }()
	devices = []TrustedDevice{}
	for rows.Next() {
		var item TrustedDevice
		var created, expiry string
		if err = rows.Scan(&item.ID, &item.UserID, &item.Username, &item.DeviceName, &item.IP, &created, &expiry); err != nil {
			return nil, err
		}
		item.CreatedAt, err = time.Parse(time.RFC3339, created)
		if err != nil {
			return nil, err
		}
		item.ExpiresAt, err = time.Parse(time.RFC3339, expiry)
		if err != nil {
			return nil, err
		}
		devices = append(devices, item)
	}
	if len(devices) > MaxTrustedDevices {
		return nil, fmt.Errorf("device store exceeds capacity")
	}
	return devices, rows.Err()
}

// RevokeDevice deletes one public device ID; repeated revocation is harmless.
func (d *DB) RevokeDevice(ctx context.Context, id string) error {
	if !validDeviceID(id) {
		return errors.New("invalid device identity")
	}
	_, err := d.db.ExecContext(ctx, "DELETE FROM trusted_devices WHERE device_id=?", id)
	return err
}

// RevokeCredential removes the caller's current normal/device credential only.
func (d *DB) RevokeCredential(ctx context.Context, sessionHash, deviceHash string) (result error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, err)
		}
	}()
	if _, err = tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE token_hash=?", sessionHash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM trusted_devices WHERE token_hash=?", deviceHash); err != nil {
		return err
	}
	return tx.Commit()
}
