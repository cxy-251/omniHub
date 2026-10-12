package audio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAudioAlbumScanAndProgress(t *testing.T) {
	tempDir := t.TempDir()
	albumDir := filepath.Join(tempDir, "测试广播剧")
	_ = os.MkdirAll(albumDir, 0755)

	// 创建多格式测试音频与封面
	_ = os.WriteFile(filepath.Join(albumDir, "01_第1集.m4a"), []byte("audio1"), 0644)
	_ = os.WriteFile(filepath.Join(albumDir, "02_第2集.mp3"), []byte("audio2"), 0644)
	_ = os.WriteFile(filepath.Join(albumDir, "03_第3集.flac"), []byte("audio3"), 0644)
	_ = os.WriteFile(filepath.Join(albumDir, "cover.jpg"), []byte("cover_image"), 0644)

	album, err := ScanAlbum(albumDir)
	if err != nil {
		t.Fatalf("failed to scan album: %v", err)
	}

	if len(album.Tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(album.Tracks))
	}
	if album.Tracks[0].Title != "01_第1集" || album.Tracks[0].Index != 1 {
		t.Errorf("unexpected track 1: %+v", album.Tracks[0])
	}
	if album.CoverPath == "" {
		t.Error("expected cover path to be found")
	}

	// 进度断点测试
	pdb, err := OpenProgressDB(filepath.Join(tempDir, "audio_progress.db"))
	if err != nil {
		t.Fatalf("failed to open progress db: %v", err)
	}
	defer pdb.Close()

	if err := pdb.SaveProgress(1, "测试广播剧:01_第1集.m4a", 125.5); err != nil {
		t.Fatalf("failed to save progress: %v", err)
	}

	offset, err := pdb.GetProgress(1, "测试广播剧:01_第1集.m4a")
	if err != nil || offset != 125.5 {
		t.Errorf("expected offset 125.5, got %f, err: %v", offset, err)
	}
}
