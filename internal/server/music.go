package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"omnihub/internal/audio"
	"omnihub/internal/auth"
	"omnihub/internal/ordering"
	"omnihub/internal/platform"
	"omnihub/internal/userstate"
)

type musicState struct {
	mu      sync.Mutex
	scanned time.Time
	key     string
	tracks  []musicTrack
}
type musicTrack struct {
	audioTrackOut
	ArtistID string
	Main     bool
}

func (r *Router) musicRequestedArtist(req *http.Request, dir, name string, tracks []*audio.Track) bool {
	key := func(s string) string {
		return strings.ToLower(strings.Map(func(c rune) rune {
			if unicode.IsLetter(c) || unicode.IsDigit(c) {
				return c
			}
			return -1
		}, s))
	}
	for _, t := range tracks[:min(5, len(tracks))] {
		if m, err := r.audioMetadata.Get(req.Context(), filepath.Join(dir, filepath.FromSlash(t.Filename)), false); err == nil && key(m.RequestedArtist) != "" && key(m.RequestedArtist) == key(name) {
			return true
		}
	}
	return false
}

// musicCatalog caches only physical catalog data; account choices and permissions
// are applied afterwards, so one user's likes/access never populate another's page.
func (r *Router) musicCatalog(req *http.Request) ([]musicTrack, error) {
	r.music.mu.Lock()
	defer r.music.mu.Unlock()
	libs := r.libMgr.ListLibraries()
	key := ""
	for _, lib := range libs {
		key += fmt.Sprint(lib.ID, lib.RootPath, lib.IsOnline)
	}
	if key == r.music.key && time.Since(r.music.scanned) < 15*time.Second {
		return r.music.tracks, nil
	}
	out := []musicTrack{}
	for _, lib := range libs {
		root, err := r.libMgr.ResolveCategoryPath(lib.ID+":"+audio.MusicRoot, "media_library/audio")
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dir := filepath.Join(root, e.Name())
			album, _, err := audio.ScanArtist(dir)
			if err != nil {
				return nil, err
			}
			artistID := lib.ID + ":" + audio.MusicRoot + "/" + e.Name()
			main := r.musicRequestedArtist(req, dir, e.Name(), album.Tracks)
			for _, t := range album.Tracks {
				if len(out) >= 100000 {
					return nil, errors.New("音乐目录超过十万首容量")
				}
				full := filepath.Join(dir, filepath.FromSlash(t.Filename))
				stat, err := os.Lstat(full)
				if err != nil || !stat.Mode().IsRegular() {
					continue
				}
				albumName := ""
				if slash := strings.IndexByte(t.Filename, '/'); slash >= 0 {
					albumName = t.Filename[:slash]
				}
				out = append(out, musicTrack{audioTrackOut: audioTrackOut{ID: artistID + "/" + t.Filename, Title: t.Title, Ext: t.Ext, Artist: e.Name(), Album: albumName}, ArtistID: artistID, Main: main})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return ordering.NaturalLess(out[i].ID, out[j].ID) })
	r.music.key, r.music.scanned, r.music.tracks = key, time.Now(), out
	return out, nil
}

func (r *Router) handleMusicTracks(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "audio", auth.TargetAll) {
		return
	}
	collection := strings.TrimPrefix(req.URL.Query().Get("path"), "@music/")
	if collection != "all" && collection != "liked" && collection != "disliked" && collection != "other" {
		http.Error(w, "invalid music collection", 400)
		return
	}
	limit, offset, err := pageBounds(req)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	catalog, err := r.musicCatalog(req)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	user := auth.UserFromContext(req.Context())
	ratings, err := r.userState.MusicRatings(req.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	groups, err := r.userState.MusicArtistGroups(req.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	query := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("q")))
	out := []audioTrackOut{}
	index := 0
	for _, t := range catalog {
		if !access.allows(t.ID) {
			continue
		}
		rating := ratings[t.ID]
		main := t.Main
		if v, ok := groups[t.ArtistID]; ok {
			main = v
		}
		if collection == "liked" && rating != 1 || collection == "disliked" && rating != -1 || collection == "other" && main {
			continue
		}
		index++
		if query != "" && !strings.Contains(strings.ToLower(t.Title+" "+t.Artist+" "+t.Album), query) {
			continue
		}
		track := t.audioTrackOut
		track.Rating = rating
		track.Index = index
		out = append(out, track)
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(out)))
	if locate := req.URL.Query().Get("locate"); locate != "" {
		for _, t := range out {
			if t.ID == locate {
				writeJSON(w, 200, []audioTrackOut{t})
				return
			}
		}
		writeJSON(w, 200, []audioTrackOut{})
		return
	}
	writeJSON(w, 200, out[min(offset, len(out)):min(offset+limit, len(out))])
}

type musicFile struct{ id, full, relative, root, artist, title string }

func (r *Router) resolveMusic(req *http.Request, id string, write bool) (musicFile, error) {
	libID, rel, ok := strings.Cut(id, ":")
	if !ok || len(id) > 2048 || !strings.HasPrefix(rel, audio.MusicRoot+"/") || filepath.ToSlash(filepath.Clean(rel)) != rel || strings.ContainsAny(rel, "\\\x00") {
		return musicFile{}, errors.New("无效音乐路径")
	}
	if !audio.SupportedExtensions[strings.ToLower(filepath.Ext(rel))] {
		return musicFile{}, errors.New("不是音乐文件")
	}
	lib := r.findLibrary(libID)
	if lib == nil || !lib.IsOnline || write && lib.ReadOnly {
		return musicFile{}, errors.New("资源库不在线或只读")
	}
	access, err := r.accessFor(req)
	if err != nil || !access.allows(id) {
		return musicFile{}, errors.New("没有此音乐的访问权限")
	}
	full, err := r.libMgr.ResolveCategoryPath(id, "media_library/audio")
	if err != nil {
		return musicFile{}, err
	}
	root, err := filepath.EvalSymlinks(lib.RootPath)
	if err != nil {
		return musicFile{}, err
	}
	if full != filepath.Join(root, filepath.FromSlash(rel)) {
		return musicFile{}, errors.New("不能通过符号链接修改音乐")
	}
	parts := strings.Split(strings.TrimPrefix(rel, audio.MusicRoot+"/"), "/")
	if len(parts) < 2 {
		return musicFile{}, errors.New("音乐必须在歌手目录内")
	}
	return musicFile{id, full, filepath.FromSlash(rel), root, parts[0], strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))}, nil
}

func (r *Router) handleMusic(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "audio", auth.TargetAll) {
		return
	}
	user := auth.UserFromContext(req.Context())
	action := strings.TrimPrefix(req.URL.Path, "/api/media/audio/music/")
	if req.Method == http.MethodGet {
		switch action {
		case "state":
			ratings, err := r.userState.MusicRatings(req.Context(), user.ID)
			if err != nil {
				http.Error(w, err.Error(), 503)
				return
			}
			writeJSON(w, 200, map[string]any{"ratings": ratings, "can_rate": user.ID > 0, "can_trash": user.Role == auth.RoleAdmin && !auth.IsWANFromContext(req.Context())})
		case "trash":
			if user.Role != auth.RoleAdmin || auth.IsWANFromContext(req.Context()) {
				http.Error(w, "administrator required", 403)
				return
			}
			rows, err := r.userState.MusicTrashRecords(req.Context(), user.ID)
			if err != nil {
				http.Error(w, err.Error(), 503)
				return
			}
			writeJSON(w, 200, rows)
		default:
			http.NotFound(w, req)
		}
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if user.ID <= 0 {
		http.Error(w, "请先登录", 401)
		return
	}
	var in struct {
		ID     string   `json:"id"`
		IDs    []string `json:"ids"`
		Rating int      `json:"rating"`
		Main   bool     `json:"main"`
	}
	if json.NewDecoder(req.Body).Decode(&in) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if action == "rating" {
		if in.Rating < -1 || in.Rating > 1 {
			http.Error(w, "invalid rating", 400)
			return
		}
		if _, err := r.resolveMusic(req, in.ID, false); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		r.music.mu.Lock()
		err := r.userState.SetMusicRating(req.Context(), user.ID, in.ID, in.Rating)
		r.music.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		writeJSON(w, 200, map[string]int{"rating": in.Rating})
		return
	}
	if action == "artist" {
		_, rel, ok := strings.Cut(in.ID, ":")
		if !ok || !audio.IsArtistDir(rel) {
			http.Error(w, "invalid artist", 400)
			return
		}
		if !r.allowPath(w, req, in.ID) {
			return
		}
		if _, err := r.libMgr.ResolveCategoryPath(in.ID, "media_library/audio"); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := r.userState.SetMusicArtistGroup(req.Context(), user.ID, in.ID, in.Main); err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if action != "trash" && action != "restore" {
		http.NotFound(w, req)
		return
	}
	if user.Role != auth.RoleAdmin || auth.IsWANFromContext(req.Context()) {
		http.Error(w, "只允许受信网络的管理员回收或恢复音乐", 403)
		return
	}
	r.music.mu.Lock()
	defer r.music.mu.Unlock()
	if action == "restore" {
		r.restoreMusic(w, req, in.ID)
		return
	}
	if len(in.IDs) == 0 || len(in.IDs) > 100 {
		http.Error(w, "每次请选择 1～100 首歌曲", 400)
		return
	}
	ratings, err := r.userState.MusicRatings(req.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	files := []musicFile{}
	seen := map[string]bool{}
	for _, id := range in.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if ratings[id] != -1 {
			http.Error(w, "只能回收当前账号已标为不喜欢的音乐", 409)
			return
		}
		file, err := r.resolveMusic(req, id, true)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		stat, err := os.Lstat(file.full)
		if err != nil || !stat.Mode().IsRegular() {
			http.Error(w, "歌曲已变化或不是普通文件", 409)
			return
		}
		files = append(files, file)
	}
	moved := []string{}
	failed := []map[string]string{}
	for _, file := range files {
		record := userstate.MusicTrash{ID: rand.Text(), UserID: user.ID, TrackID: file.id, Title: file.title, Artist: file.artist, LibraryRoot: file.root}
		if err := r.userState.CreateMusicTrash(req.Context(), record); err != nil {
			failed = append(failed, map[string]string{"id": file.id, "error": err.Error()})
			continue
		}
		receipt, opErr := platform.TrashWithReceipt(file.full)
		receipts, _ := json.Marshal([]platform.TrashReceipt{receipt})
		status, message := "trashed", ""
		if opErr != nil {
			message = opErr.Error()
			status = "failed"
			if receipt.Original != "" {
				status = "partial"
			}
		}
		if err := r.userState.UpdateMusicTrash(req.Context(), record.ID, string(receipts), status, message); err != nil {
			message = "回收记录保存失败：" + err.Error()
			if receipt.Name != "" {
				if root, e := os.OpenRoot(file.root); e == nil {
					restoreErr := platform.RestoreTrashReceipt(receipt, root, file.relative)
					root.Close()
					if restoreErr == nil {
						message += "；歌曲已恢复原位"
						receipt.Original = ""
					} else {
						message += "；请从系统回收站恢复"
					}
				}
			}
		}
		if receipt.Original != "" {
			moved = append(moved, file.id)
		}
		if message != "" {
			failed = append(failed, map[string]string{"id": file.id, "error": message})
		}
	}
	r.music.scanned = time.Time{}
	writeJSON(w, 200, map[string]any{"moved": moved, "failed": failed})
}

func (r *Router) restoreMusic(w http.ResponseWriter, req *http.Request, id string) {
	user := auth.UserFromContext(req.Context())
	records, err := r.userState.MusicTrashRecords(req.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	for _, record := range records {
		if record.ID != id {
			continue
		}
		file, err := r.resolveMusic(req, record.TrackID, true)
		if err != nil || file.root != record.LibraryRoot {
			http.Error(w, "资源库不在线、只读或位置已改变", 409)
			return
		}
		var receipts []platform.TrashReceipt
		if json.Unmarshal([]byte(record.Receipts), &receipts) != nil || len(receipts) != 1 || receipts[0].Original != file.full {
			http.Error(w, "无有效恢复凭据，请从系统回收站恢复", 409)
			return
		}
		root, err := os.OpenRoot(file.root)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		defer root.Close()
		if err := platform.RestoreTrashReceipt(receipts[0], root, file.relative); err != nil {
			http.Error(w, "未覆盖已有文件，恢复失败："+err.Error(), 409)
			return
		}
		if err := r.userState.UpdateMusicTrash(req.Context(), record.ID, record.Receipts, "restored", ""); err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		r.music.scanned = time.Time{}
		writeJSON(w, 200, map[string]string{"restored": file.id})
		return
	}
	http.NotFound(w, req)
}
