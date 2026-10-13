package games

import (
	"database/sql"
	"errors"
	"fmt"
	"omnihub/internal/storage"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB 封装游戏专区 games.db 的持久化操作（第 4.1 节）。
type DB struct {
	db *sql.DB
}

// OpenDB 打开或创建 standalone_games/games.db。
func OpenDB(gamesDir string) (*DB, error) {
	return openDB(gamesDir, false)
}

// OpenDBReadOnly 只读打开已有数据库，不建目录、不迁移。
func OpenDBReadOnly(gamesDir string) (*DB, error) { return openDB(gamesDir, true) }

func openDB(gamesDir string, readOnly bool) (*DB, error) {
	dbPath := filepath.Join(gamesDir, "games.db")
	sqliteDB, err := storage.OpenSQLite(dbPath, readOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to open games.db: %w", err)
	}

	sqliteDB.SetMaxOpenConns(2)
	sqliteDB.SetMaxIdleConns(1)
	gDB := &DB{db: sqliteDB}
	if !readOnly {
		if err := gDB.migrate(); err != nil {
			_ = sqliteDB.Close()
			return nil, fmt.Errorf("games.db migration failed: %w", err)
		}

	}

	return gDB, nil
}

// Close 关闭数据库连接。
func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS games (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		category TEXT NOT NULL,
		target TEXT NOT NULL,
		icon TEXT NOT NULL DEFAULT '',
		exe TEXT NOT NULL DEFAULT '',
		args TEXT NOT NULL DEFAULT '',
		env TEXT NOT NULL DEFAULT '',
		proton_appid TEXT NOT NULL DEFAULT '',
		peak_rss_mb INTEGER NOT NULL DEFAULT 0,
		peak_io_mbps INTEGER NOT NULL DEFAULT 0,
		play_time_sec INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS user_game_stats (
		user_id INTEGER NOT NULL,
		game_id TEXT NOT NULL,
		play_time_sec INTEGER NOT NULL DEFAULT 0,
		last_played_at TEXT NOT NULL,
		PRIMARY KEY (user_id, game_id),
		FOREIGN KEY(game_id) REFERENCES games(id) ON DELETE CASCADE
	);
	`
	_, err := d.db.Exec(schema)
	return err
}

// UpsertGame 插入或更新游戏条目。
func (d *DB) UpsertGame(g *Game) error {
	if g.ID == "" || g.Name == "" {
		return errors.New("game id and name cannot be empty")
	}

	_, err := d.db.Exec(`
		INSERT INTO games (
			id, name, category, target, icon, exe, args, env, proton_appid,
			peak_rss_mb, peak_io_mbps, play_time_sec
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			category = excluded.category,
			target = excluded.target,
			icon = excluded.icon,
			exe = excluded.exe,
			args = excluded.args,
			env = excluded.env,
			proton_appid = excluded.proton_appid`,
		g.ID, g.Name, g.Category, g.Target, g.Icon, g.Exe, g.Args, g.Env, g.ProtonAppID,
		g.PeakRSSMB, g.PeakIOMBps, g.PlayTimeSec,
	)
	return err
}

// UpdateHardwareProfile 游戏退出时回填硬件画像与游玩时长（第 4.3.2 节）。
func (d *DB) UpdateHardwareProfile(gameID string, peakRSS, peakIO, durationSec int64) error {
	_, err := d.db.Exec(`
		UPDATE games SET
			peak_rss_mb = MAX(peak_rss_mb, ?),
			peak_io_mbps = MAX(peak_io_mbps, ?),
			play_time_sec = play_time_sec + ?
		WHERE id = ?`,
		peakRSS, peakIO, durationSec, gameID,
	)
	return err
}

// RecordUserPlayTime 记录指定用户的游玩时长（第 4.4.4 节）。
func (d *DB) RecordUserPlayTime(userID int64, gameID string, durationSec int64) error {
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`
		INSERT INTO user_game_stats (user_id, game_id, play_time_sec, last_played_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, game_id) DO UPDATE SET
			play_time_sec = play_time_sec + excluded.play_time_sec,
			last_played_at = excluded.last_played_at`,
		userID, gameID, durationSec, nowStr,
	)
	return err
}

// ListGames 按照端目标（gui_only / web_only）过滤查询游戏清单（第 4.2 节）。
func (d *DB) ListGames(target string) ([]*Game, error) {
	query := "SELECT id, name, category, target, icon, exe, args, env, proton_appid, peak_rss_mb, peak_io_mbps, play_time_sec FROM games"
	var rows *sql.Rows
	var err error

	if target != "" {
		query += " WHERE target = ? ORDER BY id ASC"
		rows, err = d.db.Query(query, target)
	} else {
		query += " ORDER BY id ASC"
		rows, err = d.db.Query(query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []*Game
	for rows.Next() {
		var g Game
		if err := rows.Scan(
			&g.ID, &g.Name, &g.Category, &g.Target, &g.Icon, &g.Exe, &g.Args,
			&g.Env, &g.ProtonAppID, &g.PeakRSSMB, &g.PeakIOMBps, &g.PlayTimeSec,
		); err != nil {
			return nil, err
		}
		games = append(games, &g)
	}

	return games, nil
}
