package userstate

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
)

// ErrInvalidGamePlay rejects invalid server-owned runtime observations.
var ErrInvalidGamePlay = errors.New("invalid game play statistics")

// GameStat is one account's aggregate across copies of the same global game ID.
// Milliseconds are retained internally so short runs do not lose fractions.
type GameStat struct {
	PlayTimeSec  int64     `json:"play_time_sec"`
	LastPlayedAt time.Time `json:"last_played_at"`
}

func validGameID(id string) bool {
	if id == "" || len(id) > 512 {
		return false
	}
	for _, char := range id {
		if char < 32 || char == 127 {
			return false
		}
	}
	return true
}

// RecordGamePlay adds one completed server-owned run. Call exactly once per
// session; no client-submitted duration or polling writes are accepted.
func (d *DB) RecordGamePlay(ctx context.Context, userID int64, id string, started, ended time.Time) error {
	duration := ended.Sub(started)
	if userID <= 0 || !validGameID(id) || started.IsZero() || ended.IsZero() || duration < 0 || duration > 365*24*time.Hour {
		return ErrInvalidGamePlay
	}
	milliseconds := duration.Milliseconds()
	result, err := d.db.ExecContext(ctx, `INSERT INTO user_game_stats(user_id,game_id,play_time_ms,last_played_at) VALUES(?,?,?,?)
 ON CONFLICT(user_id,game_id) DO UPDATE SET play_time_ms=user_game_stats.play_time_ms+excluded.play_time_ms,
 last_played_at=MAX(user_game_stats.last_played_at,excluded.last_played_at)
		WHERE user_game_stats.play_time_ms>=0 AND user_game_stats.play_time_ms<=?`, userID, id, milliseconds, started.UTC().Format("2006-01-02T15:04:05.000000000Z"), int64(math.MaxInt64)-milliseconds)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("game play counter is corrupt or exhausted")
	}
	return nil
}

// GameStats reads only a bounded page of one account's aggregates. Guests have
// an empty result; absent identities never create rows or modify media indexes.
func (d *DB) GameStats(ctx context.Context, userID int64, ids []string) (map[string]GameStat, error) {
	if userID < 0 || len(ids) > 100 {
		return nil, ErrInvalidGamePlay
	}
	result := make(map[string]GameStat, len(ids))
	if userID == 0 || len(ids) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(ids))
	args := []any{userID}
	for i, id := range ids {
		if !validGameID(id) {
			return nil, ErrInvalidGamePlay
		}
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := d.db.QueryContext(ctx, "SELECT game_id,play_time_ms,last_played_at FROM user_game_stats WHERE user_id=? AND game_id IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, stamp string
		var milliseconds int64
		if err := rows.Scan(&id, &milliseconds, &stamp); err != nil {
			return nil, err
		}
		if milliseconds < 0 || len(stamp) > 40 {
			return nil, ErrInvalidGamePlay
		}
		last, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return nil, err
		}
		result[id] = GameStat{PlayTimeSec: milliseconds / 1000, LastPlayedAt: last}
	}
	return result, errors.Join(rows.Err(), rows.Close())
}
