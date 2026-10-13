package scanner

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerGamesAndManga(t *testing.T) {
	tmpDir := t.TempDir()

	// 模拟 games 目录结构
	gamesDir := filepath.Join(tmpDir, "standalone_games")
	rpgDir := filepath.Join(gamesDir, "rpg_games", "001 - TestRPG")
	steamDir := filepath.Join(gamesDir, "steam_games", "002 - TestSteam")
	_ = os.MkdirAll(rpgDir, 0755)
	_ = os.MkdirAll(steamDir, 0755)

	count, err := ScanGames(gamesDir)
	if err != nil {
		t.Fatalf("ScanGames error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 games, got %d", count)
	}

	// 模拟 manga 目录与 cbz
	mangaDir := filepath.Join(tmpDir, "media_library", "manga")
	_ = os.MkdirAll(mangaDir, 0755)
	cbzFile := filepath.Join(mangaDir, "test.cbz")
	f, _ := os.Create(cbzFile)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("cover.jpg")
	_, _ = w.Write([]byte("fake image data"))
	_ = zw.Close()
	_ = f.Close()

	mangaCount, err := ScanManga(mangaDir)
	if err != nil {
		t.Fatalf("ScanManga error: %v", err)
	}
	if mangaCount != 1 {
		t.Errorf("expected 1 manga, got %d", mangaCount)
	}

	// 模拟 shortvideo 目录结构与博主/作品
	svDir := filepath.Join(tmpDir, "media_library", "shortvideo")
	creatorVideoDir := filepath.Join(svDir, "抖音", "creator_a")
	_ = os.MkdirAll(creatorVideoDir, 0755)
	_ = os.WriteFile(filepath.Join(creatorVideoDir, "video1.mp4"), []byte("dummy mp4"), 0644)

	creatorAlbumDir := filepath.Join(svDir, "抖音", "creator_a", "album1")
	_ = os.MkdirAll(creatorAlbumDir, 0755)
	_ = os.WriteFile(filepath.Join(creatorAlbumDir, "01.jpg"), []byte("dummy image 1"), 0644)
	_ = os.WriteFile(filepath.Join(creatorAlbumDir, "02.jpg"), []byte("dummy image 2"), 0644)

	svCount, err := ScanShortVideos(svDir, true)
	if err != nil {
		t.Fatalf("ScanShortVideos error: %v", err)
	}
	if svCount != 2 {
		t.Errorf("expected 2 shortvideo posts (1 video + 1 album), got %d", svCount)
	}
}

