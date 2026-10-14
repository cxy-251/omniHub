package server

import (
	"omnihub/internal/library"
	"os"
	"path/filepath"
	"testing"
)

func TestGameStorageRejectsModuleIndexSidecarAndLogLinks(t *testing.T) {
	for _, name := range []string{".", "games.db", "games.db-wal", "games.db-shm", "logs"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			manager := library.NewManager()
			if _, err := manager.AddLibrary(root, false); err != nil {
				t.Fatal(err)
			}
			router := &Router{libMgr: manager}
			if _, err := router.gameStorageDirectory(root); err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			link := filepath.Join(root, "standalone_games", name)
			if name == "." {
				if err := os.RemoveAll(link); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			if _, err := router.gameStorageDirectory(root); err == nil {
				t.Fatal("game storage followed link")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatal("boundary check wrote outside module")
			}
		})
	}
}
