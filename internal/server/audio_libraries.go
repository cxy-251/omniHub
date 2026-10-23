package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"omnihub/internal/audio"
	"omnihub/internal/auth"
	"omnihub/internal/ordering"
)

func (r *Router) listAudioLibraries(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", 503)
		return
	}
	albums := make([]*audio.Album, 0, limit+1)
	for _, lib := range libraries {
		root, err := r.libMgr.ResolveCategoryPath(lib.ID+":media_library/audio", "media_library/audio")
		if err != nil {
			http.Error(w, "audio storage boundary invalid", 503)
			return
		}
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			http.Error(w, "audio storage unavailable", 503)
			return
		}
		dirs, total, err := r.audioCatalog.DirectoryPage(req.Context(), root, limit+1-len(albums), offset)
		if err != nil {
			http.Error(w, "audio catalog unavailable or exceeds capacity", 503)
			return
		}
		if offset >= total {
			offset -= total
			continue
		}
		offset = 0
		for _, dir := range dirs {
			if p := filepath.ToSlash(dir.Path); p == "music" || strings.HasPrefix(p, "music/") {
				continue // 音乐按歌手浏览（/api/media/audio/artists），不混在有声书专辑里
			}
			logical := lib.ID + ":" + filepath.ToSlash(filepath.Join("media_library/audio", dir.Path))
			if !access.allows(logical) {
				continue
			}
			albums = append(albums, &audio.Album{ID: logical, Title: filepath.Base(filepath.Join(root, dir.Path)), TrackCount: dir.Count, Tracks: []*audio.Track{}, LibraryID: lib.ID, LibraryName: lib.Name,
				CoverPath: r.audioAlbumCover(req, filepath.Join(root, dir.Path), logical)})
		}
		if len(albums) > limit {
			break
		}
	}
	more := len(albums) > limit
	if more {
		albums = albums[:limit]
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(more))
	writeJSON(w, 200, albums)
}

func (r *Router) handleAudioCover(w http.ResponseWriter, req *http.Request) {
	if !r.allowPath(w, req, req.URL.Query().Get("path")) {
		return
	}
	path, err := r.libMgr.ResolveCategoryPath(req.URL.Query().Get("path"), "media_library/audio")
	if err != nil {
		http.Error(w, "cover not found", 404)
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	if audio.SupportedExtensions[ext] {
		metadata, err := r.readAudioMetadata(req, true)
		if err != nil {
			http.Error(w, "embedded cover unavailable", 422)
			return
		}
		if !metadata.HasCover {
			http.Error(w, "embedded cover not found", 404)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(metadata.Picture))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = w.Write(metadata.Picture)
		return
	}
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		http.Error(w, "unsupported cover format", 400)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "cover file unavailable", 404)
		return
	}
	http.ServeFile(w, req, path)
}

// audioAlbumCover 返回专辑封面的逻辑路径（供 /api/media/audio/cover 使用）：
// 目录里的 cover/folder 图片优先，否则用第一首内嵌封面；都没有时返回空。
func (r *Router) audioAlbumCover(req *http.Request, dir, logical string) string {
	album, err := audio.ScanAlbum(dir)
	if err != nil {
		return ""
	}
	if album.CoverPath != "" {
		return logical + "/" + album.CoverPath
	}
	if len(album.Tracks) == 0 {
		return ""
	}
	first := filepath.Join(dir, album.Tracks[0].Filename)
	if meta, err := r.audioMetadata.Get(req.Context(), first, false); err == nil && meta.HasCover {
		return logical + "/" + album.Tracks[0].Filename
	}
	return ""
}

type audioTrackOut struct {
	Index    int     `json:"index"`
	Title    string  `json:"title"`
	ID       string  `json:"id"` // 逻辑路径：播放、进度、封面都用它
	Ext      string  `json:"ext"`
	Offset   float64 `json:"offset"`
	Duration float64 `json:"duration"`
	// 只在按歌词筛选（lyrics=unsynced）时给出
	LyricsStatus string `json:"lyrics_status,omitempty"`
}

// handleAudioTracks 分页列出专辑分集（自然序），q 按标题筛选；带当前账号的断点。X-Total-Count 为筛选后的总数。
func (r *Router) handleAudioTracks(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	if !r.allowPath(w, req, relPath) {
		return
	}
	fullPath, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/audio")
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, "invalid page bounds", 400)
		return
	}
	album, err := scanAudioDir(relPath, fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tracks := album.Tracks
	if q := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("q"))); q != "" {
		filtered := tracks[:0]
		for _, t := range tracks {
			if strings.Contains(strings.ToLower(t.Title), q) {
				filtered = append(filtered, t)
			}
		}
		tracks = filtered
	}
	// lyrics=unsynced：只留要单独处理的（纯文本、查不到、查询失败、没查过、搜索来的待核对；纯音乐不算），要读每首的标签（有缓存）
	statuses := map[*audio.Track]string{}
	if req.URL.Query().Get("lyrics") == "unsynced" {
		filtered := tracks[:0]
		for _, t := range tracks {
			meta, err := r.audioMetadata.Get(req.Context(), filepath.Join(fullPath, filepath.FromSlash(t.Filename)), false)
			status := meta.LyricsStatus
			if err == nil && status == "" && meta.Lyrics != "" {
				status = map[bool]string{true: "synced", false: "plain"}[audio.ParseLyrics(meta.Lyrics).Synced]
			}
			if status == "synced" && meta.LyricsMatch == "search" {
				status = "search" // 搜索来的带时间轴歌词，待核对
			}
			if status != "synced" && status != "instrumental" {
				if status == "" {
					status = "unknown"
				}
				statuses[t] = status
				filtered = append(filtered, t)
			}
		}
		tracks = filtered
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(tracks)))
	page := tracks[min(offset, len(tracks)):min(offset+limit, len(tracks))]
	base := strings.TrimSuffix(relPath, "/")
	out := make([]audioTrackOut, 0, len(page))
	ids := make([]string, 0, len(page))
	for _, t := range page {
		id := base + "/" + t.Filename
		ids = append(ids, id)
		out = append(out, audioTrackOut{Index: t.Index, Title: t.Title, ID: id, Ext: t.Ext, LyricsStatus: statuses[t]})
	}
	if r.progressDB != nil {
		if progress, err := r.progressDB.ProgressFor(auth.UserFromContext(req.Context()).ID, ids); err == nil {
			for i := range out {
				if p, ok := progress[out[i].ID]; ok {
					out[i].Offset, out[i].Duration = p.Offset, p.Duration
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAudioRecent 返回当前账号最近听过的音轨（有权限且文件还在的），供「继续收听」。
func (r *Router) handleAudioRecent(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "audio", auth.TargetAll) {
		return
	}
	user := auth.UserFromContext(req.Context())
	type recentOut struct {
		audio.Progress
		Title      string `json:"title"`
		AlbumID    string `json:"album_id"`
		AlbumTitle string `json:"album_title"`
	}
	out := []recentOut{}
	if r.progressDB == nil || user.ID <= 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}
	limit := 12
	if n, err := strconv.Atoi(req.URL.Query().Get("limit")); err == nil && n > 0 && n <= 50 {
		limit = n
	}
	list, err := r.progressDB.Recent(user.ID, limit*3)
	if err != nil {
		http.Error(w, "progress store unavailable", 503)
		return
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", 503)
		return
	}
	for _, p := range list {
		if len(out) >= limit {
			break
		}
		if !access.allows(p.AudioID) {
			continue
		}
		full, err := r.libMgr.ResolveCategoryPath(p.AudioID, "media_library/audio")
		if err != nil {
			continue
		}
		if info, err := os.Lstat(full); err != nil || !info.Mode().IsRegular() {
			continue
		}
		album := p.AudioID[:strings.LastIndex(p.AudioID, "/")]
		out = append(out, recentOut{Progress: p, Title: strings.TrimSuffix(filepath.Base(full), filepath.Ext(full)), AlbumID: album, AlbumTitle: filepath.Base(filepath.Dir(full))})
	}
	writeJSON(w, http.StatusOK, out)
}

// scanAudioDir 扫描一个音声目录：music/<歌手> 这一层展开成整个歌手的播放列表（单曲 + 各张专辑），
// 这样「循环这个歌手」和「点一首歌、播完接着下一首单曲或专辑」都用同一个列表；其余目录照旧只看本层。
func scanAudioDir(logical, full string) (*audio.Album, error) {
	if _, rel, ok := strings.Cut(logical, ":"); ok && audio.IsArtistDir(rel) {
		album, _, err := audio.ScanArtist(full)
		return album, err
	}
	return audio.ScanAlbum(full)
}

type artistOut struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	SingleCount int    `json:"single_count"`
	AlbumCount  int    `json:"album_count"`
	TrackCount  int    `json:"track_count"`
	CoverPath   string `json:"cover_path"`
	LibraryName string `json:"library_name"`
}

// artistCover 用歌手列表里第一首带封面的歌（最多看 5 首）当封面。
func (r *Router) artistCover(req *http.Request, dir, logical string, tracks []*audio.Track) string {
	for _, t := range tracks[:min(5, len(tracks))] {
		if meta, err := r.audioMetadata.Get(req.Context(), filepath.Join(dir, filepath.FromSlash(t.Filename)), false); err == nil && meta.HasCover {
			return logical + "/" + t.Filename
		}
	}
	return ""
}

// handleAudioArtists 列出各资源库 music/ 下的歌手（按名字自然序），带单曲数、专辑数和封面。
func (r *Router) handleAudioArtists(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "audio", auth.TargetAll) {
		return
	}
	libraries, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", 503)
		return
	}
	out := []artistOut{}
	for _, lib := range libraries {
		root, err := r.libMgr.ResolveCategoryPath(lib.ID+":"+audio.MusicRoot, "media_library/audio")
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || len(out) >= 5000 {
				continue
			}
			logical := lib.ID + ":" + audio.MusicRoot + "/" + e.Name()
			if !access.allows(logical) {
				continue
			}
			dir := filepath.Join(root, e.Name())
			album, sections, err := audio.ScanArtist(dir)
			if err != nil || len(album.Tracks) == 0 {
				continue
			}
			out = append(out, artistOut{ID: logical, Title: e.Name(), SingleCount: sections[0].Count, AlbumCount: len(sections) - 1,
				TrackCount: len(album.Tracks), CoverPath: r.artistCover(req, dir, logical, album.Tracks), LibraryName: lib.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return ordering.NaturalLess(strings.ToLower(out[i].Title), strings.ToLower(out[j].Title))
	})
	writeJSON(w, 200, out)
}

// handleAudioArtist 返回一个歌手的分段：第一段是单曲，其后每张专辑一段（id 是专辑目录，offset 是在歌手播放列表里的起点）。
func (r *Router) handleAudioArtist(w http.ResponseWriter, req *http.Request) {
	relPath := req.URL.Query().Get("path")
	if !r.allowPath(w, req, relPath) {
		return
	}
	_, rel, _ := strings.Cut(relPath, ":")
	full, err := r.libMgr.ResolveCategoryPath(relPath, "media_library/audio")
	if err != nil || !audio.IsArtistDir(rel) {
		http.Error(w, "artist not found", http.StatusNotFound)
		return
	}
	album, sections, err := audio.ScanArtist(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type sectionOut struct {
		audio.ArtistSection
		ID        string `json:"id"`
		CoverPath string `json:"cover_path"`
	}
	out := make([]sectionOut, 0, len(sections))
	for _, sec := range sections {
		o := sectionOut{ArtistSection: sec}
		if sec.Album != "" {
			o.ID = relPath + "/" + sec.Album
		}
		if sec.Count > 0 {
			o.CoverPath = r.artistCover(req, full, relPath, album.Tracks[sec.Offset:sec.Offset+sec.Count])
		}
		out = append(out, o)
	}
	writeJSON(w, 200, map[string]any{"id": relPath, "title": filepath.Base(full), "track_count": len(album.Tracks), "sections": out})
}
