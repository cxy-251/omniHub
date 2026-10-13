package gallery

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGalleryVolumeAndMetadata(t *testing.T) {
	tempDir := t.TempDir()
	galleryDir := filepath.Join(tempDir, "gallery")
	originalsDir := filepath.Join(galleryDir, "originals")

	// 1. 初始化分卷管理器与元数据库
	vm, err := NewVolumeManager(originalsDir)
	if err != nil {
		t.Fatalf("failed to init volume manager: %v", err)
	}
	defer vm.Close()

	meta, err := OpenMetaDB(galleryDir)
	if err != nil {
		t.Fatalf("failed to open meta db: %v", err)
	}
	defer meta.Close()

	photoID := "photo_sha256_mock_123"
	originalBytes := []byte("fake_original_jpeg_binary_data")

	// 2. 写入原图分卷
	volName, err := vm.WriteOriginal(photoID, originalBytes)
	if err != nil {
		t.Fatalf("failed to write original: %v", err)
	}
	if volName == "" {
		t.Error("expected valid volume name")
	}

	// 3. 从只读卷中读取
	readBytes, err := vm.ReadOriginal(volName, photoID)
	if err != nil {
		t.Fatalf("failed to read original: %v", err)
	}
	if string(readBytes) != string(originalBytes) {
		t.Errorf("expected original data %s, got %s", string(originalBytes), string(readBytes))
	}

	// 4. 插入元数据
	photo := &Photo{
		ID:          photoID,
		Filename:    "test.jpg",
		SizeBytes:   int64(len(originalBytes)),
		MimeType:    "image/jpeg",
		Width:       1920,
		Height:      1080,
		Volume:      volName,
		ColorFamily: 3,
		ColorHex:    "#336699",
		IsFavorite:  true,
		CreatedAt:   time.Now().UTC(),
	}
	if err := meta.UpsertPhoto(photo); err != nil {
		t.Fatalf("failed to upsert photo: %v", err)
	}

	// 5. 保存并获取缩略图
	thumbBytes := []byte("fake_480px_thumbnail_data")
	thumb := &Thumbnail{
		PhotoID:  photoID,
		SizeType: "m",
		Width:    480,
		Height:   270,
		Data:     thumbBytes,
	}
	if err := meta.SaveThumbnail(thumb); err != nil {
		t.Fatalf("failed to save thumbnail: %v", err)
	}

	readThumb, err := meta.GetThumbnail(photoID)
	if err != nil {
		t.Fatalf("failed to get thumbnail: %v", err)
	}
	if string(readThumb) != string(thumbBytes) {
		t.Errorf("expected thumb %s, got %s", string(thumbBytes), string(readThumb))
	}

	// 6. 软删除打标（废纸篓，第 3.3.3 节）
	if err := meta.SoftDelete(photoID, true); err != nil {
		t.Fatalf("failed to soft delete: %v", err)
	}
	p, err := meta.GetPhoto(photoID)
	if err != nil || p == nil {
		t.Fatalf("failed to get photo: %v", err)
	}
	if p.DeletedAt == nil {
		t.Error("expected non-nil deleted_at after soft delete")
	}

	// 恢复软删除
	if err := meta.SoftDelete(photoID, false); err != nil {
		t.Fatalf("failed to restore photo: %v", err)
	}
	p, _ = meta.GetPhoto(photoID)
	if p.DeletedAt != nil {
		t.Error("expected nil deleted_at after restore")
	}

	// 7. 测试分页查询
	photos, err := meta.ListPhotos(false, 10, 0)
	if err != nil {
		t.Fatalf("ListPhotos failed: %v", err)
	}
	if len(photos) != 1 {
		t.Errorf("expected 1 photo from pagination, got %d", len(photos))
	}

	// 8. 测试 ReadOriginalDirect 纯只读解耦读取
	directBytes, err := ReadOriginalDirect(originalsDir, volName, photoID)
	if err != nil {
		t.Fatalf("ReadOriginalDirect failed: %v", err)
	}
	if string(directBytes) != string(originalBytes) {
		t.Errorf("expected directBytes %s, got %s", string(originalBytes), string(directBytes))
	}
}
