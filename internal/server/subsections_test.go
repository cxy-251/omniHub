package server

import (
	"testing"

	"omnihub/internal/auth"
)

// 子分类分级：nsfw 默认仅管理员，单独设置优先，其余沿用大分类等级。
func TestSubsectionAccess(t *testing.T) {
	c := &accessChecker{level: 0, stored: map[string]int{"text": 0, "audio": 2, "manga": 0}, memo: map[string]bool{}}
	cases := []struct {
		path string
		want bool
	}{
		{"lib:media_library/novels/nsfw/类/书.epub", false},
		{"lib:media_library/novels/standard/类/书.epub", true},
		{"lib:media_library/audio/nsfw", false},       // 专辑就是一级目录本身
		{"lib:media_library/audio/standard/x/1.mp3", false}, // 音频大分类要求等级 2
		{"lib:media_library/docs/readme.md", true},
		{"media_library/novels/nsfw/a.epub", false}, // 不带库前缀的路径
		{"lib:media_library/manga/nsfw/a.cbz", false},
		{"lib:media_library/manga/a.cbz", false}, // 旧布局平铺的漫画按 NSFW
		{"lib:media_library/manga/standard/a.cbz", true},
	}
	for _, tc := range cases {
		if got := c.allows(tc.path); got != tc.want {
			t.Errorf("guest allows(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
	c.stored["text/novels/nsfw"] = 0
	c.memo = map[string]bool{}
	if !c.allows("lib:media_library/novels/nsfw/类/书.epub") {
		t.Error("explicit level 0 must open nsfw to guests")
	}
	admin := &accessChecker{level: auth.LevelAdmin, stored: map[string]int{}, memo: map[string]bool{}}
	if !admin.allows("lib:media_library/audio/nsfw/1.mp3") {
		t.Error("admin must see nsfw")
	}
}

// 标了 NSFW 的短视频平台、相册图库默认仅管理员；单独设置后按设置；没标的沿用大分类。
func TestMarkedNSFWAccess(t *testing.T) {
	c := &accessChecker{level: 1, stored: map[string]int{"shortvideo": 0, "gallery": 0},
		nsfw: map[string]bool{"shortvideo/X": true, "gallery/X": true}, memo: map[string]bool{}}
	if c.shortVideo("X/作者/2026-10-01_a_1.mp4") || c.galleryCollection("X") {
		t.Error("NSFW platform and gallery must be admin-only by default")
	}
	if !c.shortVideo("YouTube/频道/2026-10-01_a_abcdefghijk.mp4") || !c.galleryCollection("Twice") || !c.galleryCollection("") {
		t.Error("unmarked platforms and galleries follow the section level")
	}
	c.stored["shortvideo/X"] = 1
	c.memo = map[string]bool{}
	if !c.shortVideo("X/作者/2026-10-01_a_1.mp4") || c.galleryCollection("X") {
		t.Error("an explicit level opens only that subsection")
	}
	c.stored["shortvideo/mainstream"] = 2
	c.stored["games"] = 0
	c.stored["games/other"] = 3
	c.memo = map[string]bool{}
	if c.shortVideo("抖音/作者/a.mp4") || c.shortVideo("快手/作者/a.mp4") || c.shortVideo("小红书/作者/a.mp4") || c.shortVideo("TikTok/作者/a.mp4") {
		t.Error("merged mainstream shortvideo platforms must share shortvideo/mainstream level")
	}
	if !c.shortVideo("MV/歌手/a.mp4") {
		t.Error("non-mainstream platform MV should still follow section level")
	}
	if !c.gameCategory("retro") || !c.gameCategory("3ds") || c.gameCategory("rpg") || c.gameCategory("steam") {
		t.Error("emulator games (retro/3ds) and other games must follow their respective subsections")
	}
}
