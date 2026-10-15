package shortvideo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 清理原目录前必须逐字节核对：目录有任何差异都不能调用回收站。
func TestVerifyAndTrashRefusesChangedAlbum(t *testing.T) {
	dir := t.TempDir()
	album := filepath.Join(dir, "抖音", "博主", "2024-01-01_图集_1234567890")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"01.webp", "02.webp"} {
		if err := os.WriteFile(filepath.Join(album, name), []byte("image "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Scan(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	id := "抖音/博主/2024-01-01_图集_1234567890"
	post, _, _ := s.Post(id)
	if err := s.ImportAlbum(dir, post); err != nil {
		t.Fatal(err)
	}
	post, _, _ = s.Post(id)

	trashed := ""
	trash := func(path string) error { trashed = path; return nil }

	// 修改一张图：拒绝。
	if err := os.WriteFile(filepath.Join(album, "02.webp"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyAndTrash(dir, post, trash); err == nil || trashed != "" {
		t.Fatalf("changed album must be kept, err=%v trashed=%q", err, trashed)
	}
	// 恢复内容但多一个文件：拒绝。
	os.WriteFile(filepath.Join(album, "02.webp"), []byte("image 02.webp"), 0o644)
	os.WriteFile(filepath.Join(album, "note.txt"), []byte("x"), 0o644)
	if err := s.VerifyAndTrash(dir, post, trash); err == nil || trashed != "" {
		t.Fatalf("album with extra file must be kept, err=%v trashed=%q", err, trashed)
	}
	// 完全一致：调用回收站并改为从数据库读取。
	os.Remove(filepath.Join(album, "note.txt"))
	if err := s.VerifyAndTrash(dir, post, trash); err != nil || trashed != album {
		t.Fatalf("identical album should be trashed, err=%v trashed=%q", err, trashed)
	}
	if data, err := s.AlbumImage(id, 2); err != nil || string(data) != "image 02.webp" {
		t.Fatalf("image must be served from albums.db, got %q %v", data, err)
	}
}
