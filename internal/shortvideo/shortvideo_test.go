package shortvideo

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestShortvideoStorage(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := OpenStorage(tempDir)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer storage.Close()

	// 1. 缩略图存取
	thumbData := []byte("fake_video_first_frame")
	if err := storage.SaveThumbnail("post_001", thumbData); err != nil {
		t.Fatalf("failed to save thumbnail: %v", err)
	}

	gotThumb, err := storage.GetThumbnail("post_001")
	if err != nil || string(gotThumb) != string(thumbData) {
		t.Errorf("expected thumb %s, got %s, err %v", string(thumbData), string(gotThumb), err)
	}

	// 2. 图集存取
	imgData := []byte("fake_album_img_1")
	if err := storage.SaveAlbumImage("post_002", 1, imgData); err != nil {
		t.Fatalf("failed to save album image: %v", err)
	}

	gotImg, err := storage.GetAlbumImage("post_002", 1)
	if err != nil || string(gotImg) != string(imgData) {
		t.Errorf("expected img %s, got %s, err %v", string(imgData), string(gotImg), err)
	}
}

func TestVideoStreamRange206(t *testing.T) {
	tempDir := t.TempDir()
	videoFile := filepath.Join(tempDir, "sample.mp4")
	content := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	if err := os.WriteFile(videoFile, content, 0644); err != nil {
		t.Fatalf("failed to write test video: %v", err)
	}

	// 发起 Range 请求 (bytes=0-9)
	req := httptest.NewRequest("GET", "/video", nil)
	req.Header.Set("Range", "bytes=0-9")
	rec := httptest.NewRecorder()

	if err := StreamVideo(rec, req, videoFile); err != nil {
		t.Fatalf("stream video failed: %v", err)
	}

	if rec.Code != http.StatusPartialContent {
		t.Errorf("expected status 206, got %d", rec.Code)
	}
	if rec.Body.String() != "0123456789" {
		t.Errorf("expected slice '0123456789', got '%s'", rec.Body.String())
	}
}

func TestOversizedAlbumAndThumbnailDoNotLoadBlobs(t *testing.T) {
	s, err := OpenStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.thumbDB.Exec("DROP TABLE thumbnails; CREATE VIEW thumbnails AS SELECT 'big' target_id, zeroblob(2097153) data"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.albumDB.Exec("DROP TABLE album_images; CREATE VIEW album_images AS SELECT 'big' post_id, 1 order_index, zeroblob(33554433) data"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetThumbnail("big"); !errors.Is(err, ErrImageCapacity) {
		t.Fatalf("unbounded thumbnail: %v", err)
	}
	if _, err := s.GetAlbumImage("big", 1); !errors.Is(err, ErrImageCapacity) {
		t.Fatalf("unbounded album image: %v", err)
	}
}
