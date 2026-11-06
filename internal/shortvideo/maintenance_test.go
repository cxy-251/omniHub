package shortvideo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanAndCleanPartials(t *testing.T) {
	dir := t.TempDir()
	creatorDir := filepath.Join(dir, "抖音", "测试博主")
	if err := os.MkdirAll(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Normal files that must NOT be touched
	if err := os.WriteFile(filepath.Join(creatorDir, "2026-01-01_正常视频_1234567890.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "downloaded.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Partial / empty files that SHOULD be detected
	partFile := filepath.Join(creatorDir, "unfinished.mp4.part")
	zeroFile := filepath.Join(creatorDir, "empty.mp4")
	if err := os.WriteFile(partFile, []byte("partdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zeroFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanPartials([]string{dir})
	if err != nil {
		t.Fatalf("PlanPartials failed: %v", err)
	}
	if plan.Files != 2 || plan.Bytes != 8 {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	var trashed []string
	n, err := CleanPartials(context.Background(), plan, func(p string) error {
		trashed = append(trashed, p)
		return os.Remove(p)
	}, nil)
	if err != nil || n != 2 || len(trashed) != 2 {
		t.Fatalf("CleanPartials failed: n=%d err=%v trashed=%v", n, err, trashed)
	}
	if _, err := os.Stat(filepath.Join(creatorDir, "2026-01-01_正常视频_1234567890.mp4")); err != nil {
		t.Fatalf("normal video should remain untouched: %v", err)
	}
}
