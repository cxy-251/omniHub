package text

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTextChunk(t *testing.T) {
	tempDir := t.TempDir()
	txtPath := filepath.Join(tempDir, "sample.md")
	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\n"
	if err := os.WriteFile(txtPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test text: %v", err)
	}

	// 读取第 2 行到第 4 行
	res, err := ReadTextChunk(txtPath, 2, 3)
	if err != nil {
		t.Fatalf("failed to read chunk: %v", err)
	}

	if len(res.Lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(res.Lines))
	}
	if res.Lines[0] != "Line 2" || res.Lines[2] != "Line 4" {
		t.Errorf("unexpected chunk content: %v", res.Lines)
	}
	if res.TotalLines != 5 {
		t.Errorf("expected 5 total lines, got %d", res.TotalLines)
	}
	if !res.HasMore {
		t.Error("expected has_more to be true")
	}
}

func TestReadEpubEntry(t *testing.T) {
	tempDir := t.TempDir()
	epubPath := filepath.Join(tempDir, "sample.epub")

	file, err := os.Create(epubPath)
	if err != nil {
		t.Fatalf("failed to create epub file: %v", err)
	}

	zw := zip.NewWriter(file)
	entryWriter, err := zw.Create("OEBPS/chapter1.xhtml")
	if err != nil {
		t.Fatalf("failed to create entry: %v", err)
	}
	expectedHTML := "<p>Chapter 1 Content</p>"
	_, _ = entryWriter.Write([]byte(expectedHTML))
	_ = zw.Close()
	_ = file.Close()

	// 提取 entry 内容
	data, err := ReadEpubEntry(epubPath, "OEBPS/chapter1.xhtml")
	if err != nil {
		t.Fatalf("failed to read epub entry: %v", err)
	}
	if string(data) != expectedHTML {
		t.Errorf("expected %s, got %s", expectedHTML, string(data))
	}
}
