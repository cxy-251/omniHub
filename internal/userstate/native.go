package userstate

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"time"
)

// NativePosition records one externally opened media revision for an account.
// IDs/revisions are digests; filesystem paths and privileged credentials are never stored.
type NativePosition struct {
	ID       string  `json:"id"`
	Version  string  `json:"version"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
	Paused   bool    `json:"paused"`
	Volume   float64 `json:"volume"`
	Rate     float64 `json:"rate"`
}

func nativeDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func validNativePosition(p NativePosition) bool {
	return nativeDigest(p.ID) && nativeDigest(p.Version) && !math.IsNaN(p.Position) && !math.IsInf(p.Position, 0) && p.Position >= 0 && p.Position <= p.Duration && !math.IsNaN(p.Duration) && !math.IsInf(p.Duration, 0) && p.Duration > 0 && p.Duration <= 31536000 && !math.IsNaN(p.Volume) && !math.IsInf(p.Volume, 0) && p.Volume >= 0 && p.Volume <= 100 && !math.IsNaN(p.Rate) && !math.IsInf(p.Rate, 0) && p.Rate >= 0.25 && p.Rate <= 3
}

// SaveNative stores changed state once and retains the latest 256 revisions per
// account. Separate revision keys prevent a late old-file save overwriting a new file.
func (d *DB) SaveNative(ctx context.Context, userID int64, p NativePosition) (result error) {
	if userID <= 0 || !validNativePosition(p) {
		return ErrInvalidProgress
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
	changed, err := tx.ExecContext(ctx, `INSERT INTO native_progress(user_id,media_id,file_version,position,duration,paused,volume,rate,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id,media_id,file_version) DO UPDATE SET
 position=excluded.position,duration=excluded.duration,paused=excluded.paused,volume=excluded.volume,rate=excluded.rate,updated_at=excluded.updated_at
 WHERE native_progress.position!=excluded.position OR native_progress.duration!=excluded.duration OR native_progress.paused!=excluded.paused OR native_progress.volume!=excluded.volume OR native_progress.rate!=excluded.rate`, userID, p.ID, p.Version, p.Position, p.Duration, p.Paused, p.Volume, p.Rate, time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"))
	if err != nil {
		return err
	}
	count, err := changed.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM native_progress WHERE user_id=? AND rowid IN
 (SELECT rowid FROM native_progress WHERE user_id=? ORDER BY updated_at DESC,rowid DESC LIMIT -1 OFFSET 256)`, userID, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Native reads exactly one account/media/revision without writes; unseen or
// changed files return nil. No file IO occurs at this database boundary.
func (d *DB) Native(ctx context.Context, userID int64, id, version string) (*NativePosition, error) {
	if userID <= 0 || !nativeDigest(id) || !nativeDigest(version) {
		return nil, ErrInvalidProgress
	}
	p := NativePosition{ID: id, Version: version}
	err := d.db.QueryRowContext(ctx, `SELECT position,duration,paused,volume,rate FROM native_progress WHERE user_id=? AND media_id=? AND file_version=?`, userID, id, version).Scan(&p.Position, &p.Duration, &p.Paused, &p.Volume, &p.Rate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validNativePosition(p) {
		return nil, ErrInvalidProgress
	}
	return &p, nil
}
