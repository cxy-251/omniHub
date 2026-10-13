package manga

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func createMockCBZ(t *testing.T, cbzPath string) {
	file, err := os.Create(cbzPath)
	if err != nil {
		t.Fatalf("failed to create cbz: %v", err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	// 写入 metadata.json
	metaData := []byte(`{
		"id": "manga_123",
		"title": "测试漫画",
		"author": "作者A",
		"tags": ["恋爱", "全彩"],
		"local_chapters": 10,
		"is_complete": true
	}`)
	w, _ := zw.Create("metadata.json")
	_, _ = w.Write(metaData)

	// 写入 cover.jpg
	wCover, _ := zw.Create("cover.jpg")
	_, _ = wCover.Write([]byte("mock_cover_bytes"))

	// 写入 01_话/001.webp
	wP1, _ := zw.Create("01_话/001.webp")
	_, _ = wP1.Write([]byte("mock_page_1"))

	// 写入 01_话/002.webp
	wP2, _ := zw.Create("01_话/002.webp")
	_, _ = wP2.Write([]byte("mock_page_2"))
}

func TestMangaCBZAndProgress(t *testing.T) {
	tempDir := t.TempDir()
	cbzFile := filepath.Join(tempDir, "test.cbz")
	createMockCBZ(t, cbzFile)

	// 1. 测试 CBZ 零落盘读取
	reader, err := OpenCBZ(cbzFile)
	if err != nil {
		t.Fatalf("failed to open cbz: %v", err)
	}
	defer reader.Close()

	// 验证封面多级回退
	coverBytes, err := reader.ReadCoverBytes("")
	if err != nil || string(coverBytes) != "mock_cover_bytes" {
		t.Errorf("expected cover 'mock_cover_bytes', got %s, err %v", string(coverBytes), err)
	}

	// 随机读取第 2 页
	p2Bytes, err := reader.ReadEntry("01_话/002.webp")
	if err != nil || string(p2Bytes) != "mock_page_2" {
		t.Errorf("expected page 'mock_page_2', got %s, err %v", string(p2Bytes), err)
	}

	// 2. 测试元数据扫描构建
	m, err := ScanMangaFromCBZ(cbzFile)
	if err != nil {
		t.Fatalf("failed to scan manga: %v", err)
	}
	if m.ID != "manga_123" || m.Title != "测试漫画" || len(m.Tags) != 2 {
		t.Errorf("unexpected scanned metadata: %+v", m)
	}

	// 3. 测试数据库与进度持久化
	db, err := OpenDB(tempDir)
	if err != nil {
		t.Fatalf("failed to open manga db: %v", err)
	}
	defer db.Close()

	if err := db.UpsertManga(m); err != nil {
		t.Fatalf("failed to upsert manga: %v", err)
	}

	// 4. 自定义设为封面测试
	if err := db.SetCustomCover(m.ID, "01_话/002.webp"); err != nil {
		t.Fatalf("failed to set custom cover: %v", err)
	}

	// 5. 条漫像素级进度测试
	prog := &MangaProgress{
		UserID:            1,
		MangaID:           m.ID,
		ChapterIndex:      2,
		PageIndex:         5,
		ScrollOffsetRatio: 0.65,
	}
	if err := db.SaveProgress(prog); err != nil {
		t.Fatalf("failed to save progress: %v", err)
	}

	gotProg, err := db.GetProgress(1, m.ID)
	if err != nil || gotProg == nil {
		t.Fatalf("failed to get progress: %v", err)
	}
	if gotProg.ChapterIndex != 2 || gotProg.PageIndex != 5 || gotProg.ScrollOffsetRatio != 0.65 {
		t.Errorf("unexpected progress: %+v", gotProg)
	}
}

func TestNaturalImageOrderAndMetadataErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "natural.cbz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, name := range []string{"10.jpg", "2.jpg", "metadata.json"} {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("invalid metadata")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := OpenCBZ(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	images := r.ListImageEntries()
	if len(images) != 2 || images[0] != "2.jpg" {
		t.Fatalf("not naturally sorted: %v", images)
	}
	images[0] = "mutated"
	if r.ListImageEntries()[0] != "2.jpg" {
		t.Fatal("caller mutated cached order")
	}
	if r.GetCoverEntry("metadata.json") != "2.jpg" {
		t.Fatal("metadata exposed as cover")
	}
	if _, err := ScanMangaFromCBZ(path); err == nil {
		t.Fatal("malformed metadata silently discarded")
	}
}
