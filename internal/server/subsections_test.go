package server

import (
	"testing"

	"omnihub/internal/auth"
)

// 子分类分级：nsfw 默认仅管理员，单独设置优先，其余沿用大分类等级。
func TestSubsectionAccess(t *testing.T) {
	c := &accessChecker{level: 0, stored: map[string]int{"text": 0, "audio": 2}, memo: map[string]bool{}}
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
