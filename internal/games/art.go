package games

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/storage"
)

const maxArtBytes = 4 << 20

// ArtStore 把游戏封面图缓存在资源库的 standalone_games/games.db（game_art 表），
// 记录来源文件与修改时间，来源不变时直接返回，避免每次都解析 exe。
type ArtStore struct{ db *sql.DB }

// OpenArtStore 打开（必要时创建）games.db 中的 game_art 表；只用于可写资源库。
func OpenArtStore(gamesDir string) (*ArtStore, error) {
	db, err := storage.OpenSQLite(filepath.Join(gamesDir, "games.db"), false)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS game_art (
		id           TEXT PRIMARY KEY,
		source       TEXT NOT NULL,
		source_mtime INTEGER NOT NULL,
		mime         TEXT NOT NULL,
		data         BLOB NOT NULL
	)`); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &ArtStore{db: db}, nil
}

func (s *ArtStore) Close() error { return s.db.Close() }

// Cached 返回缓存的图；来源文件不存在或已修改时视为未命中。
func (s *ArtStore) Cached(id string) ([]byte, string, bool) {
	var source, mime string
	var mtime int64
	var data []byte
	if err := s.db.QueryRow("SELECT source, source_mtime, mime, data FROM game_art WHERE id=?", id).Scan(&source, &mtime, &mime, &data); err != nil {
		return nil, "", false
	}
	if source != "" {
		info, err := os.Stat(source)
		if err != nil || info.ModTime().UnixNano() != mtime {
			return nil, "", false
		}
	}
	return data, mime, true
}

// Put 保存一张图；source 为空表示「没有可用图片」（data 为空），来源出现前不再重算。
func (s *ArtStore) Put(id string, art Art) error {
	var mtime int64
	if art.Source != "" {
		if info, err := os.Stat(art.Source); err == nil {
			mtime = info.ModTime().UnixNano()
		}
	}
	_, err := s.db.Exec(`INSERT INTO game_art (id, source, source_mtime, mime, data) VALUES (?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET source=excluded.source, source_mtime=excluded.source_mtime, mime=excluded.mime, data=excluded.data`,
		id, art.Source, mtime, art.MIME, art.Data)
	return err
}

// Art 是一张封面图及其来源文件。
type Art struct {
	Source string
	MIME   string
	Data   []byte
}

// FindArt 依次尝试：omni.json/常见图片文件 → Steam 封面缓存（steam_appid.txt 或 proton_appid）→ exe 内嵌图标。
func FindArt(gameDir, category string, meta Meta, icon, steamRoot string) (Art, error) {
	if icon != "" {
		if art, err := readArtFile(filepath.Join(gameDir, filepath.FromSlash(icon))); err == nil {
			return art, nil
		}
	}
	if steamRoot != "" {
		appID := meta.ProtonApp()
		if data, err := os.ReadFile(filepath.Join(gameDir, "steam_appid.txt")); err == nil && appID == "" {
			appID = strings.TrimSpace(string(data))
		}
		if appID != "" && !strings.ContainsAny(appID, "/\\.") {
			cache := filepath.Join(steamRoot, "appcache", "librarycache", appID)
			for _, name := range []string{"library_600x900.jpg", "header.jpg", "library_header_schinese.jpg", "library_header.jpg"} {
				if art, err := readArtFile(filepath.Join(cache, name)); err == nil {
					return art, nil
				}
			}
		}
	}
	exe, err := FindExecutable(gameDir, category, meta)
	if err == nil && strings.HasSuffix(strings.ToLower(exe), ".exe") {
		full := filepath.Join(gameDir, filepath.FromSlash(exe))
		if data, mime, err := ExeIcon(full); err == nil {
			return Art{Source: full, MIME: mime, Data: data}, nil
		}
	}
	return Art{}, errors.New("no artwork")
}

func readArtFile(path string) (Art, error) {
	f, err := os.Open(path)
	if err != nil {
		return Art{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxArtBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxArtBytes {
		return Art{}, errors.New("invalid artwork file")
	}
	return Art{Source: path, MIME: http.DetectContentType(data), Data: data}, nil
}
