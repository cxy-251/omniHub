package games

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWebEntriesChooseRealAssetsAndRejectAmbiguousPayloads(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "www"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "www", "index.html"), []byte("html"), 0600); err != nil {
		t.Fatal(err)
	}
	game := &Game{Target: "web_only", Category: "rpg"}
	kind, entry, err := WebEntry(root, game)
	if err != nil || kind != "html" || entry != "www/index.html" {
		t.Fatalf("HTML selection: %s %s %v", kind, entry, err)
	}
	game.Exe = "save/private.html"
	if _, _, err := WebEntry(root, game); err == nil {
		t.Fatal("private configured entry accepted")
	}
	game = &Game{Target: "web_only", Category: "retro"}
	for _, name := range []string{"first.nes", "second.nes"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("ROM"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := WebEntry(root, game); err == nil {
		t.Fatal("multiple ROMs guessed silently")
	}
	game.Exe = "second.nes"
	if kind, entry, err := WebEntry(root, game); err != nil || kind != "retro" || entry != game.Exe {
		t.Fatalf("configured ROM: %s %s %v", kind, entry, err)
	}
	if err := os.Symlink("www", filepath.Join(root, "linked")); err == nil {
		game = &Game{Target: "web_only", Category: "rpg", Exe: "linked/index.html"}
		if _, _, err := WebEntry(root, game); err == nil {
			t.Fatal("configured linked entry accepted")
		}
	}
	for _, name := range []string{"../private", "./www/index.html", "www/../index.html", "C:/file", "www\\index.html", "save/file", "SAVE/file", "www/.env", "www/omni.json", "logs/file"} {
		if PublicWebAsset(name) {
			t.Fatalf("private/escaping path allowed: %s", name)
		}
	}
}
