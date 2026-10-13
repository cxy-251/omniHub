package audio

import (
	"math"
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

func TestProgressSkipsUnchangedOffsetAndRejectsInvalidValues(t *testing.T) {
	db, err := OpenProgressDB(filepath.Join(t.TempDir(), "progress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveProgress(1, "library:media_library/audio/track.mp3", 10); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProgress(1, "library:media_library/audio/track.mp3", 10); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("identical progress rewrote database row")
	}
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		if err := db.SaveProgress(1, "track", value); err == nil {
			t.Fatal("invalid progress accepted")
		}
	}
	if err := db.SaveProgress(0, "track", 10); err == nil {
		t.Fatal("anonymous shared progress accepted")
	}
}
