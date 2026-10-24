package shortvideo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 频道下按团分的视频子文件夹收成「频道 / 团名」一个分类，不当成图集，也不漏掉；YouTube 文件名里的 11 位 ID 不算进标题。
func TestScanVideoSubfolders(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("YouTube/STARSHIP/IVE/2022-04-05_IVE 'LOVE DIVE' MV_Y8JFxS1HlDo.mp4")
	write("YouTube/STARSHIP/2020-01-01_公司宣传片_abcdefghijk.mp4")
	write("抖音/博主/2024-01-01_图集_1234567890/01.webp")
	write("B站/UP主/2019-12-31_【官方 MV】Never Gonna Give You Up_BV1GJ411x7h7.mp4")
	s, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stats, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Videos != 3 || stats.Albums != 1 {
		t.Fatalf("videos=%d albums=%d, want 3 and 1", stats.Videos, stats.Albums)
	}
	if p, _, err := s.Post("B站/UP主/2019-12-31_【官方 MV】Never Gonna Give You Up_BV1GJ411x7h7.mp4"); err != nil || p.Title != "【官方 MV】Never Gonna Give You Up" {
		t.Fatalf("bilibili post = %+v, %v", p, err)
	}
	creators, err := s.Creators("YouTube")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, c := range creators {
		names[c.ID] = c.Name
	}
	if names["YouTube/STARSHIP/IVE"] != "STARSHIP / IVE" || names["YouTube/STARSHIP"] != "STARSHIP" {
		t.Fatalf("creators = %v", names)
	}
	post, _, err := s.Post("YouTube/STARSHIP/IVE/2022-04-05_IVE 'LOVE DIVE' MV_Y8JFxS1HlDo.mp4")
	if err != nil || post.CreatorID != "YouTube/STARSHIP/IVE" || post.Title != "IVE 'LOVE DIVE' MV" {
		t.Fatalf("post = %+v, %v", post, err)
	}
}
