package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoveryReusesRecordsInvalidatesMetadataAndNeverCreatesIndex(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "rpg_games", "001 - Folder")
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(folder, "omni.json")
	if err := os.WriteFile(config, []byte(`{"name":"First"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cache := NewGameDiscovery()
	defer cache.Close()
	first, err := cache.Open(context.Background(), root, []string{"rpg"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := first.GetGame(context.Background(), "001")
	if err != nil || value.Name != "First" || !value.Available {
		t.Fatalf("discovery: %+v %v", value, err)
	}
	value.Name = "mutated"
	next, err := cache.Open(context.Background(), root, []string{"rpg"})
	if err != nil || next.entry != first.entry {
		t.Fatal("unchanged request rebuilt metadata")
	}
	value, err = next.GetGame(context.Background(), "001")
	if err != nil || value.Name != "First" {
		t.Fatal("returned record polluted cache")
	}
	if err := os.WriteFile(config, []byte(`{"name":"Changed title"}`), 0600); err != nil {
		t.Fatal(err)
	}
	first.entry.checked = time.Now().Add(-11 * time.Second)
	changed, err := cache.Open(context.Background(), root, []string{"rpg"})
	if err != nil {
		t.Fatal(err)
	}
	value, err = changed.GetGame(context.Background(), "001")
	if err != nil || value.Name != "Changed title" || changed.entry == first.entry {
		t.Fatal("changed metadata was not rebuilt")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Open(ctx, root, []string{"rpg"}); err == nil {
		t.Fatal("cancelled cache hit accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "games.db")); !os.IsNotExist(err) {
		t.Fatal("discovery created an index")
	}
	if cache.bytes > discoveryBudget {
		t.Fatal("cache retained excess metadata")
	}
	cache.Close()
	if _, err := cache.Open(context.Background(), root, []string{"rpg"}); err == nil {
		t.Fatal("closed cache repopulated")
	}
}
