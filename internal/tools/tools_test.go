package tools

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestClipboardManager(t *testing.T) {
	cm := NewClipboardManager(3)

	cm.Push("item 1")
	cm.Push("item 2")
	cm.Push("item 3")
	cm.Push("item 4") // 触发队列溢出淘汰

	list := cm.List()
	if len(list) != 3 {
		t.Errorf("expected 3 items, got %d", len(list))
	}
	if list[0].Content != "item 4" {
		t.Errorf("expected latest item 'item 4', got %s", list[0].Content)
	}

	cm.Clear()
	if len(cm.List()) != 0 {
		t.Error("expected empty list after clear")
	}
}

func TestSharedFilesAndZip(t *testing.T) {
	tempDir := t.TempDir()

	// 1. 分块写入测试 (chunk 0 + chunk 1)
	if err := WriteChunk(tempDir, "test.txt", 0, []byte("Hello, ")); err != nil {
		t.Fatalf("write chunk 0 failed: %v", err)
	}
	if err := WriteChunk(tempDir, "test.txt", 1, []byte("World!")); err != nil {
		t.Fatalf("write chunk 1 failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tempDir, "test.txt"))
	if err != nil || string(content) != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got '%s', err: %v", string(content), err)
	}

	// 2. 文件列表测试
	files, err := ListSharedFiles(tempDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("expected 1 file, got %v", files)
	}
	if files[0].Name != "test.txt" || files[0].SizeBytes != int64(len("Hello, World!")) {
		t.Errorf("unexpected file metadata: %+v", files[0])
	}

	// 3. 流式 ZIP 打包测试
	var buf bytes.Buffer
	if err := StreamZipArchive(tempDir, []string{"test.txt"}, &buf); err != nil {
		t.Fatalf("stream zip failed: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("failed to read created zip: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "test.txt" {
		t.Errorf("unexpected files in zip: %v", zr.File)
	}
}

func TestGetSystemMetrics(t *testing.T) {
	m := GetSystemMetrics("/")
	if m.NumCPU <= 0 {
		t.Errorf("expected NumCPU > 0, got %d", m.NumCPU)
	}
}
