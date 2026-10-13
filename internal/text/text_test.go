package text

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if res.TotalLines != -1 {
		t.Errorf("total should remain unknown before EOF, got %d", res.TotalLines)
	}
	if !res.HasMore {
		t.Error("expected has_more to be true")
	}
	next, err := ReadTextChunkContext(context.Background(), txtPath, 5, 3, res.NextOffset)
	if err != nil || len(next.Lines) != 1 || next.Lines[0] != "Line 5" || next.TotalLines != 5 || next.HasMore {
		t.Fatalf("cursor did not resume at final line: %+v, %v", next, err)
	}
}

func TestTextPageStopsBeforeOversizedTailAndBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded.txt")
	if err := os.WriteFile(path, []byte("first\n"+strings.Repeat("x", maxLineBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ReadTextChunkContext(context.Background(), path, 1, 1, 0)
	if err != nil || first.NextOffset != 6 || !first.HasMore {
		t.Fatalf("first page read past its boundary: %+v %v", first, err)
	}
	if _, err := ReadTextChunkContext(context.Background(), path, 2, 1, first.NextOffset); err == nil {
		t.Fatal("oversized line accepted")
	}
	if _, err := ReadTextChunk(path, 1, 501); !errors.Is(err, ErrBounds) {
		t.Fatal("unbounded page accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadTextChunkContext(ctx, path, 1, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestTextByteBudgetResumesWithoutLosingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.txt")
	line := strings.Repeat("x", 700000) + "\n"
	if err := os.WriteFile(path, []byte(line+line+line+"last"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ReadTextChunkContext(context.Background(), path, 1, 100, 0)
	if err != nil || len(first.Lines) != 2 || first.NextOffset != int64(2*len(line)) {
		t.Fatalf("byte budget not enforced: %+v %v", first, err)
	}
	next, err := ReadTextChunkContext(context.Background(), path, 3, 100, first.NextOffset)
	if err != nil || len(next.Lines) != 2 || next.Lines[1] != "last" || next.HasMore || next.TotalLines != 4 {
		t.Fatalf("page boundary lost text: count=%d err=%v", len(next.Lines), err)
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
