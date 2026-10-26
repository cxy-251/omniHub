package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"omnihub/internal/catalog"
	"omnihub/internal/library"
	"omnihub/internal/manga"
	"omnihub/internal/userstate"
)

// 旧布局（manga/ 下平铺）自动搬进 nsfw/，standard/ 不动，同名冲突的不搬；阅读进度和点赞跟着改路径。
func TestMigrateMangaLayout(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media_library", "manga")
	for _, f := range []string{"a.cbz", "b.cbz", "standard/s.cbz", "nsfw/b.cbz"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state, err := userstate.Open(filepath.Join(t.TempDir(), "user_state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	ctx := context.Background()
	old := "lib:media_library/manga/a.cbz"
	if err := state.SaveManga(ctx, &manga.MangaProgress{UserID: 1, MangaID: old, ChapterIndex: 1, PageIndex: 5}); err != nil {
		t.Fatal(err)
	}
	if err := state.SetLike(ctx, 1, "manga", old, true); err != nil {
		t.Fatal(err)
	}
	r := &Router{mangaCatalog: catalog.New(".cbz"), userState: state}
	if err := r.migrateMangaLayout(ctx, &library.Library{ID: "lib", Name: "t", RootPath: root}, dir); err != nil {
		t.Fatal(err)
	}
	for f, want := range map[string]bool{"a.cbz": false, "nsfw/a.cbz": true, "b.cbz": true, "nsfw/b.cbz": true, "standard/s.cbz": true} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", f, err == nil, want)
		}
	}
	moved := "lib:media_library/manga/nsfw/a.cbz"
	if p, err := state.Manga(ctx, 1, moved); err != nil || p == nil || p.PageIndex != 5 {
		t.Errorf("progress not moved: %+v %v", p, err)
	}
	if likes, _ := state.Likes(ctx, 1, "manga"); len(likes) != 1 || likes[0] != moved {
		t.Errorf("likes = %v", likes)
	}
}
