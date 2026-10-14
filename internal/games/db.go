package games

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"omnihub/internal/storage"
	"path"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB 封装游戏专区 games.db 的持久化操作（第 4.1 节）。
type DB struct {
	db        *sql.DB
	hasRelDir bool
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
			return nil, errors.Join(fmt.Errorf("games.db migration failed: %w", err), sqliteDB.Close())
		}

	}
	columns, err := sqliteDB.Query("PRAGMA table_info(games)")
	if err != nil {
		return nil, errors.Join(err, sqliteDB.Close())
	}
	for columns.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue any
		if err := columns.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			return nil, errors.Join(err, columns.Close(), sqliteDB.Close())
		}
		if name == "rel_dir" {
			gDB.hasRelDir = true
		}
	}
	if err := errors.Join(columns.Err(), columns.Close()); err != nil {
		return nil, errors.Join(err, sqliteDB.Close())
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
		rel_dir TEXT NOT NULL DEFAULT '',
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
	if _, err := d.db.Exec(schema); err != nil {
		return err
	}
	rows, err := d.db.Query("SELECT name FROM pragma_table_info('games') WHERE name='rel_dir'")
	if err != nil {
		return err
	}
	found := rows.Next()
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if !found {
		_, err = d.db.Exec("ALTER TABLE games ADD COLUMN rel_dir TEXT NOT NULL DEFAULT ''")
	}
	return err
}

// UpsertGame 插入或更新游戏条目。
func (d *DB) UpsertGame(g *Game) error {
	return d.UpsertGames([]*Game{g})
}

// UpsertGames validates and publishes a scan atomically, preserving unrelated
// records and existing play/hardware statistics; it does not delete user assets.
func (d *DB) UpsertGames(list []*Game) error {
	if len(list) > 20000 {
		return errors.New("game index exceeds capacity")
	}
	for _, g := range list {
		if err := ValidateIndexGame(g); err != nil {
			return err
		}
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	statement, err := tx.Prepare(gameUpsertSQL)
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}
	for _, g := range list {
		if err := upsertGame(statement, g); err != nil {
			return errors.Join(err, statement.Close(), tx.Rollback())
		}
	}
	if err := statement.Close(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// ValidateIndexGame bounds persisted metadata and checks category, target and
// relative paths before a scanner opens or modifies the index.
func ValidateIndexGame(g *Game) error {
	if g == nil || g.ID == "" || g.Name == "" || len(g.ID) > 512 || len(g.Name) > 4096 {
		return errors.New("game id and name cannot be empty")
	}
	if !ValidCategory(g.Category) || (g.Target != "gui_only" && g.Target != "web_only") {
		return errors.New("invalid game category or target")
	}
	if len(g.RelDir) > 4096 || len(g.Exe) > 4096 || len(g.Icon) > 4096 || len(g.ProtonAppID) > 128 || len(g.Args)+len(g.Env) > 32<<10 {
		return errors.New("game index fields exceed capacity")
	}
	if g.RelDir != "" && (!filepath.IsLocal(filepath.FromSlash(g.RelDir)) || strings.Contains(g.RelDir, "\\") || path.Clean(g.RelDir) != g.RelDir || strings.Count(g.RelDir, "/") != 1 || !strings.HasPrefix(g.RelDir, g.Category+"_games/")) {
		return errors.New("invalid indexed game directory")
	}
	if g.RelDir != "" {
		id, _ := FolderIdentity(path.Base(g.RelDir))
		if id != g.ID {
			return errors.New("indexed directory prefix differs from game ID")
		}
	}
	for _, value := range []string{g.ID, g.Name, g.RelDir, g.Exe, g.Icon, g.Args, g.Env, g.ProtonAppID} {
		if strings.ContainsRune(value, 0) {
			return errors.New("game metadata contains NUL")
		}
	}
	if g.Exe != "" && (!filepath.IsLocal(filepath.FromSlash(g.Exe)) || strings.Contains(g.Exe, "\\")) {
		return errors.New("game executable escapes its folder")
	}
	return nil
}

const gameUpsertSQL = `
		INSERT INTO games (
			id, name, category, target, icon, exe, args, env, proton_appid,
			peak_rss_mb, peak_io_mbps, play_time_sec, rel_dir
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			category = excluded.category,
			target = excluded.target,
			icon = excluded.icon,
			exe = excluded.exe,
			args = excluded.args,
			env = excluded.env,
			proton_appid = excluded.proton_appid,
			rel_dir = CASE WHEN excluded.rel_dir='' THEN games.rel_dir ELSE excluded.rel_dir END
		WHERE games.name IS NOT excluded.name OR games.category IS NOT excluded.category
		   OR games.target IS NOT excluded.target OR games.icon IS NOT excluded.icon
		   OR games.exe IS NOT excluded.exe OR games.args IS NOT excluded.args
		   OR games.env IS NOT excluded.env OR games.proton_appid IS NOT excluded.proton_appid
		   OR (excluded.rel_dir!='' AND games.rel_dir IS NOT excluded.rel_dir)`

func upsertGame(statement *sql.Stmt, g *Game) error {
	_, err := statement.Exec(
		g.ID, g.Name, g.Category, g.Target, g.Icon, g.Exe, g.Args, g.Env, g.ProtonAppID,
		g.PeakRSSMB, g.PeakIOMBps, g.PlayTimeSec, g.RelDir,
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

// GetGame reads one index record without loading the full catalog. Legacy
// read-only indexes return an empty RelDir and never migrate on a query.
func (d *DB) GetGame(ctx context.Context, id string) (*Game, error) {
	if id == "" || len(id) > 512 {
		return nil, errors.New("invalid game identity")
	}
	relDir := "''"
	if d.hasRelDir {
		relDir = boundedCatalogColumn("rel_dir", 4096)
	}
	var g Game
	columns := []string{boundedCatalogColumn("id", 512), boundedCatalogColumn("name", 4096), boundedCatalogColumn("category", 32), boundedCatalogColumn("target", 16), boundedCatalogColumn("icon", 4096), boundedCatalogColumn("exe", 4096), boundedCatalogColumn("args", 32<<10), boundedCatalogColumn("env", 32<<10), boundedCatalogColumn("proton_appid", 128), "peak_rss_mb", "peak_io_mbps", "play_time_sec", relDir}
	err := d.db.QueryRowContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM games WHERE id=?", id).Scan(&g.ID, &g.Name, &g.Category, &g.Target, &g.Icon, &g.Exe, &g.Args, &g.Env, &g.ProtonAppID, &g.PeakRSSMB, &g.PeakIOMBps, &g.PlayTimeSec, &g.RelDir)
	if err != nil {
		return nil, err
	}
	return &g, nil
}
