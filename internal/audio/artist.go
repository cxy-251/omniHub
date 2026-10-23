package audio

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"omnihub/internal/ordering"
)

// MusicRoot 是音乐在音声分类里的位置：music/<歌手>/ 下直接放单曲，<歌手>/<专辑>/ 放整张专辑。
const MusicRoot = "media_library/audio/music"

// IsArtistDir 判断库内路径（不含库 ID）是不是 music/<歌手> 这一层。
func IsArtistDir(rel string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	parent, name := path.Split(rel)
	return strings.TrimSuffix(parent, "/") == MusicRoot && name != "" && !strings.HasPrefix(name, ".")
}

// ArtistSection 是歌手播放列表里的一段：单曲（Album 为空）或一张专辑；Offset 是这一段第一首在整个列表里的位置（0 起）。
type ArtistSection struct {
	Album  string `json:"album"`
	Offset int    `json:"offset"`
	Count  int    `json:"count"`
}

// ScanArtist 把歌手目录展开成一个播放列表：先是单曲（目录里直接放的歌），再按专辑名自然序接各张专辑的歌（专辑内按曲序）。
// 曲目的 Filename 带专辑子目录（专辑/01 歌名.opus）。只看一层子目录。
func ScanArtist(dir string) (*Album, []ArtistSection, error) {
	singles, err := ScanAlbum(dir)
	if err != nil {
		return nil, nil, err
	}
	sections := []ArtistSection{{Offset: 0, Count: len(singles.Tracks)}}
	f, err := os.Open(dir)
	if err != nil {
		return nil, nil, err
	}
	entries, err := f.ReadDir(2001)
	f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	if len(entries) > 2000 {
		return nil, nil, errors.New("artist exceeds album capacity")
	}
	var albums []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			albums = append(albums, e.Name())
		}
	}
	sort.Slice(albums, func(i, j int) bool { return ordering.NaturalLess(albums[i], albums[j]) })
	out := singles
	for _, name := range albums {
		album, err := ScanAlbum(filepath.Join(dir, name))
		if err != nil || len(album.Tracks) == 0 {
			continue
		}
		sections = append(sections, ArtistSection{Album: name, Offset: len(out.Tracks), Count: len(album.Tracks)})
		for _, t := range album.Tracks {
			t.Filename = name + "/" + t.Filename
			t.RelPath = t.Filename
			t.Index = len(out.Tracks) + 1
			out.Tracks = append(out.Tracks, t)
		}
	}
	out.TrackCount = len(out.Tracks)
	return out, sections, nil
}
