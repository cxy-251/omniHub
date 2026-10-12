package sc2

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSC2MapListAndArgs(t *testing.T) {
	tempDir := t.TempDir()
	mapsDir := filepath.Join(tempDir, "maps")
	_ = os.MkdirAll(mapsDir, 0755)

	_ = os.WriteFile(filepath.Join(mapsDir, "LostTemple.SC2Map"), []byte("map1"), 0644)
	_ = os.WriteFile(filepath.Join(mapsDir, "LostTemple.png"), []byte("thumb"), 0644)

	maps, err := ListMaps(mapsDir)
	if err != nil || len(maps) != 1 {
		t.Fatalf("expected 1 map, got %v, err: %v", maps, err)
	}
	if maps[0].Name != "LostTemple" || maps[0].ThumbPath == "" {
		t.Errorf("unexpected map item: %+v", maps[0])
	}

	opt := MatchOptions{
		MapName:    "LostTemple",
		PlayerRace: "P",
		Difficulty: "Hard",
		Mods:       []string{"5xHarvest"},
	}
	args, err := BuildMatchArgs(opt, tempDir)
	if err != nil {
		t.Fatalf("build args failed: %v", err)
	}

	if len(args) != 10 || args[3] != "LostTemple" {
		t.Errorf("unexpected args: %v", args)
	}
}
