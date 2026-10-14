package games

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGamesDBAndFiltering(t *testing.T) {
	tempDir := t.TempDir()
	db, err := OpenDB(tempDir)
	if err != nil {
		t.Fatalf("failed to open games db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	// 1. 插入独立大作 (gui_only)
	gameGUI := &Game{
		ID:       "001",
		Name:     "Thronefall",
		Category: "steam",
		Target:   "gui_only",
		Exe:      "game.x86_64",
	}
	if err := db.UpsertGame(gameGUI); err != nil {
		t.Fatalf("failed to upsert gui game: %v", err)
	}

	// 2. 插入模拟器游戏 (web_only)
	gameWeb := &Game{
		ID:       "002",
		Name:     "Pokemon",
		Category: "retro",
		Target:   "web_only",
		Exe:      "rom.gba",
	}
	if err := db.UpsertGame(gameWeb); err != nil {
		t.Fatalf("failed to upsert web game: %v", err)
	}

	// 3. 过滤查询 gui_only 游戏
	guiList, err := db.ListCatalogPage(context.Background(), "gui_only", "", "", 100)
	if err != nil {
		t.Fatalf("failed to list gui games: %v", err)
	}
	if len(guiList) != 1 || guiList[0].ID != "001" {
		t.Errorf("expected 1 gui game with id 001, got %v", guiList)
	}

	// 4. 过滤查询 web_only 游戏
	webList, err := db.ListCatalogPage(context.Background(), "web_only", "", "", 100)
	if err != nil {
		t.Fatalf("failed to list web games: %v", err)
	}
	if len(webList) != 1 || webList[0].ID != "002" {
		t.Errorf("expected 1 web game with id 002, got %v", webList)
	}

	// 5. 硬件画像回填与时长累计
	if err := db.UpdateHardwareProfile("001", 1024, 50, 3600); err != nil {
		t.Fatalf("failed to update hardware profile: %v", err)
	}
	games, err := db.ListCatalogPage(context.Background(), "gui_only", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if games[0].PeakRSSMB != 1024 || games[0].PlayTimeSec != 3600 {
		t.Errorf("unexpected updated profile: %+v", games[0])
	}

	// 6. 用户独立时长累计
	if err := db.RecordUserPlayTime(10, "001", 1800); err != nil {
		t.Fatalf("failed to record user play time: %v", err)
	}
}

func TestPrepareSaveDirIsolation(t *testing.T) {
	tempDir := t.TempDir()
	gameDir := filepath.Join(tempDir, "001 - Game")
	if err := os.Mkdir(gameDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Admin 用户应直通 gameDir/save
	adminSave, err := PrepareSaveDir(gameDir, 1, true)
	if err != nil {
		t.Fatalf("prepare admin save failed: %v", err)
	}
	expectedAdmin := filepath.Join(gameDir, "save")
	if adminSave != expectedAdmin {
		t.Errorf("expected admin save %s, got %s", expectedAdmin, adminSave)
	}

	// 普通用户应隔离在 gameDir/save/users/42
	userSave, err := PrepareSaveDir(gameDir, 42, false)
	if err != nil {
		t.Fatalf("prepare user save failed: %v", err)
	}
	expectedUser := filepath.Join(gameDir, "save", "users", "42")
	if userSave != expectedUser {
		t.Errorf("expected user save %s, got %s", expectedUser, userSave)
	}
}
