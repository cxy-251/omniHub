package userstate

import (
	"context"
	"database/sql"
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

// GameProfile 是一款游戏的硬件画像（全局，不分账号）：内存与磁盘取历次最高，CPU 取最近一次够长的运行。
type GameProfile struct {
	PeakMemMB     int64   `json:"peak_mem_mb"`
	LastPeakMemMB int64   `json:"last_peak_mem_mb"`
	PeakIOMBps    float64 `json:"peak_io_mbps"`
	AvgCPUPct     float64 `json:"avg_cpu_pct"`
}

// RecordGameProfile 合并一次运行的画像。seconds 不到 2 分钟的运行不更新平均 CPU（多是只开了启动器）。
func (d *DB) RecordGameProfile(ctx context.Context, id string, peakMemMB int64, peakIOMBps, avgCPUPct float64, seconds int64) error {
	if !validGameID(id) || peakMemMB < 0 || peakIOMBps < 0 || avgCPUPct < 0 {
		return ErrInvalidGamePlay
	}
	keepCPU := seconds < 120
	_, err := d.db.ExecContext(ctx, `INSERT INTO game_profiles(game_id,peak_mem_mb,peak_io_mbps,avg_cpu_pct,last_peak_mem_mb,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(game_id) DO UPDATE SET peak_mem_mb=MAX(game_profiles.peak_mem_mb,excluded.peak_mem_mb),
		peak_io_mbps=MAX(game_profiles.peak_io_mbps,excluded.peak_io_mbps),
		avg_cpu_pct=CASE WHEN ? THEN game_profiles.avg_cpu_pct ELSE excluded.avg_cpu_pct END,
		last_peak_mem_mb=excluded.last_peak_mem_mb, updated_at=excluded.updated_at`,
		id, peakMemMB, peakIOMBps, avgCPUPct, peakMemMB, time.Now().UTC().Format(time.RFC3339), keepCPU)
	return err
}

// GameProfiles 读取一批游戏的画像（最多 100 个）。
func (d *DB) GameProfiles(ctx context.Context, ids []string) (map[string]GameProfile, error) {
	if len(ids) > 100 {
		return nil, ErrInvalidGamePlay
	}
	result := make(map[string]GameProfile, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i], args[i] = "?", id
	}
	rows, err := d.db.QueryContext(ctx, "SELECT game_id,peak_mem_mb,last_peak_mem_mb,peak_io_mbps,avg_cpu_pct FROM game_profiles WHERE game_id IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var p GameProfile
		if err := rows.Scan(&id, &p.PeakMemMB, &p.LastPeakMemMB, &p.PeakIOMBps, &p.AvgCPUPct); err != nil {
			return nil, err
		}
		result[id] = p
	}
	return result, rows.Err()
}

// 网页游戏存档：游戏在沙箱里用不了浏览器存储，运行时把 localStorage 的读写转到这里，按账号、按游戏保存。
const (
	MaxWebSaveValue = 8 << 20  // 单个键
	MaxWebSaveTotal = 64 << 20 // 单个游戏合计
)

var ErrWebSaveTooLarge = errors.New("web game save too large")

// WebGameSave 读一个存档键；没有时 ok 为假。
func (d *DB) WebGameSave(ctx context.Context, userID int64, gameID, key string) (value string, ok bool, err error) {
	err = d.db.QueryRowContext(ctx, `SELECT value FROM web_game_saves WHERE user_id=? AND game_id=? AND key=?`, userID, gameID, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// WebGameSaves 返回某账号某游戏的存档键值（localStorage 替身用），不含 "file:" 开头的存档文件（可能有几 MB，
// 由 __savefile 按文件单独读）。
func (d *DB) WebGameSaves(ctx context.Context, userID int64, gameID string) (map[string]string, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT key, value FROM web_game_saves WHERE user_id=? AND game_id=? AND substr(key,1,5)<>'file:'`, userID, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetWebGameSave 写入或删除（remove 为真）一个存档键。
func (d *DB) SetWebGameSave(ctx context.Context, userID int64, gameID, key, value string, remove bool) error {
	if userID <= 0 || gameID == "" || len(gameID) > 512 || key == "" || len(key) > 1024 {
		return ErrInvalidProgress
	}
	if remove {
		_, err := d.db.ExecContext(ctx, `DELETE FROM web_game_saves WHERE user_id=? AND game_id=? AND key=?`, userID, gameID, key)
		return err
	}
	if len(value) > MaxWebSaveValue {
		return ErrWebSaveTooLarge
	}
	var total int64
	if err := d.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(LENGTH(value)),0) FROM web_game_saves WHERE user_id=? AND game_id=? AND key<>?`, userID, gameID, key).Scan(&total); err != nil {
		return err
	}
	if total+int64(len(value)) > MaxWebSaveTotal {
		return ErrWebSaveTooLarge
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO web_game_saves(user_id,game_id,key,value,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(user_id,game_id,key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		userID, gameID, key, value, time.Now().UTC().Format(time.RFC3339))
	return err
}
