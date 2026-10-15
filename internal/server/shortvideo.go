package server

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/platform"
	"omnihub/internal/shortvideo"
)

const shortVideoCategory = "media_library/shortvideo"

// svStore 返回某个在线资源库的短视频库连接及目录。只读库只能打开已存在的数据库。
func (r *Router) svStore(lib *library.Library) (*shortvideo.Store, string, error) {
	dir, err := r.moduleDirectory(lib.RootPath, shortVideoCategory, shortvideo.DatabaseFiles...)
	if err != nil {
		return nil, "", err
	}
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	if r.closed {
		return nil, "", errors.New("router is closed")
	}
	key := lib.RootPath + ":" + strconv.FormatBool(lib.ReadOnly)
	if s := r.svStores[key]; s != nil {
		return s, dir, nil
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, "", err
	}
	s, err := shortvideo.Open(dir, lib.ReadOnly)
	if err != nil {
		return nil, "", err
	}
	r.svStores[key] = s
	return s, dir, nil
}

// svLibrary 解析请求中的 library_id，要求库在线。
func (r *Router) svLibrary(w http.ResponseWriter, req *http.Request) *library.Library {
	if req.URL.Query().Get("library_id") == "" {
		http.Error(w, "library_id required", http.StatusBadRequest)
		return nil
	}
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return nil
	}
	return libs[0]
}

// validPostID 拒绝绝对路径、越界与反斜杠；ID 必须是 平台/博主/作品 三段。
func validPostID(id string) bool {
	return id != "" && len(id) < 2048 && !strings.ContainsAny(id, "\\\x00") &&
		path.Clean(id) == id && !strings.HasPrefix(id, "/") && !strings.HasPrefix(id, "../") && strings.Count(id, "/") == 2
}

type svPost struct {
	shortvideo.Post
	LibraryID string `json:"library_id"`
	Liked     bool   `json:"liked"`
}

// svLikes 返回当前账号的点赞集合；未登录（游客）为空。
func (r *Router) svLikes(req *http.Request) map[string]bool {
	user := auth.UserFromContext(req.Context())
	likes, err := r.userState.ShortVideoLikes(req.Context(), user.ID)
	if err != nil {
		return map[string]bool{}
	}
	return likes
}

// onlyLiked 保留点赞过的作品键（liked=1 时用，多联放映的「我的点赞」频道）。
func onlyLiked(keys []svKey, likes map[string]bool) []svKey {
	out := keys[:0]
	for _, k := range keys {
		if likes[k.ID] {
			out = append(out, k)
		}
	}
	return out
}

// svKey 是跨库合并后的作品键。
type svKey struct {
	shortvideo.Key
	store *shortvideo.Store
	lib   string
}

// svMergedKeys 合并各库符合筛选的作品键，按发布日期倒序；未建索引的库跳过。
func (r *Router) svMergedKeys(w http.ResponseWriter, req *http.Request, filter shortvideo.Filter) ([]svKey, bool) {
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return nil, false
	}
	var all []svKey
	for _, lib := range libs {
		store, _, err := r.svStore(lib)
		if err != nil {
			continue
		}
		keys, err := store.Keys(filter)
		if err != nil {
			http.Error(w, "short video index unavailable", http.StatusServiceUnavailable)
			return nil, false
		}
		for _, k := range keys {
			all = append(all, svKey{k, store, lib.ID})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Published != all[j].Published {
			return all[i].Published > all[j].Published
		}
		return all[i].ID > all[j].ID
	})
	return all, true
}

// handleShortVideoPosts 跨库分页列出作品，X-Total-Count 给出总数，客户端可直接跳到任意页。
func (r *Router) handleShortVideoPosts(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil || limit > 200 || offset < 0 {
		http.Error(w, "invalid page bounds", http.StatusBadRequest)
		return
	}
	q := req.URL.Query()
	keys, ok := r.svMergedKeys(w, req, shortvideo.Filter{Platform: q.Get("platform"), CreatorID: q.Get("creator"), Kind: shortvideo.Kind(q.Get("kind"))})
	if !ok {
		return
	}
	likes := r.svLikes(req)
	if q.Get("liked") == "1" {
		keys = onlyLiked(keys, likes)
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(keys)))
	page := keys[min(offset, len(keys)):min(offset+limit, len(keys))]
	// 按库分组读取详情，再按合并后的顺序输出。
	byStore := make(map[*shortvideo.Store][]string)
	for _, k := range page {
		byStore[k.store] = append(byStore[k.store], k.ID)
	}
	found := make(map[string]shortvideo.Post, len(page))
	for store, ids := range byStore {
		posts, err := store.PostsByID(ids)
		if err != nil {
			http.Error(w, "short video index unavailable", http.StatusServiceUnavailable)
			return
		}
		for _, p := range posts {
			found[p.ID] = p
		}
	}
	out := make([]svPost, 0, len(page))
	for _, k := range page {
		if p, ok := found[k.ID]; ok {
			out = append(out, svPost{Post: p, LibraryID: k.lib, Liked: likes[k.ID]})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleShortVideoCreators 合并各库的博主，同名博主（同一 ID）作品数相加。
func (r *Router) handleShortVideoCreators(w http.ResponseWriter, req *http.Request) {
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	merged := make(map[string]*shortvideo.Creator)
	for _, lib := range libs {
		store, _, err := r.svStore(lib)
		if err != nil {
			continue
		}
		list, err := store.Creators(req.URL.Query().Get("platform"))
		if err != nil {
			http.Error(w, "short video index unavailable", http.StatusServiceUnavailable)
			return
		}
		for _, c := range list {
			if m := merged[c.ID]; m != nil {
				m.Posts += c.Posts
			} else {
				copy := c
				merged[c.ID] = &copy
			}
		}
	}
	out := make([]*shortvideo.Creator, 0, len(merged))
	for _, c := range merged {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		return out[i].Posts > out[j].Posts
	})
	writeJSON(w, http.StatusOK, out)
}

func (r *Router) svPostFromRequest(w http.ResponseWriter, req *http.Request) (*shortvideo.Store, string, shortvideo.Post, bool) {
	lib := r.svLibrary(w, req)
	if lib == nil {
		return nil, "", shortvideo.Post{}, false
	}
	id := req.URL.Query().Get("id")
	if !validPostID(id) {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return nil, "", shortvideo.Post{}, false
	}
	store, dir, err := r.svStore(lib)
	if err != nil {
		http.Error(w, "short video index unavailable", http.StatusServiceUnavailable)
		return nil, "", shortvideo.Post{}, false
	}
	post, found, err := store.Post(id)
	if err != nil || !found {
		http.NotFound(w, req)
		return nil, "", shortvideo.Post{}, false
	}
	return store, dir, post, true
}

func (r *Router) handleShortVideoThumbnail(w http.ResponseWriter, req *http.Request) {
	store, _, post, ok := r.svPostFromRequest(w, req)
	if !ok {
		return
	}
	data, err := store.Thumbnail(post.ID)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// handleShortVideoStream 流式播放视频或图集背景音乐（id 为图集时 music=1 选择其 mp3）。
func (r *Router) handleShortVideoStream(w http.ResponseWriter, req *http.Request) {
	_, dir, post, ok := r.svPostFromRequest(w, req)
	if !ok {
		return
	}
	rel, contentType := post.ID, "video/mp4"
	if post.Kind == shortvideo.KindAlbum {
		if post.Music == "" || req.URL.Query().Get("music") != "1" {
			http.NotFound(w, req)
			return
		}
		rel, contentType = post.Music, "audio/mpeg"
	}
	full, err := shortvideo.ResolveFile(dir, rel)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	if err := shortvideo.StreamVideo(w, req, full, contentType); err != nil {
		http.Error(w, "media file unavailable", http.StatusServiceUnavailable)
	}
}

// handleShortVideoAlbumImage 返回图集第 index 张图片：原目录在磁盘时读文件，否则读 albums.db。
func (r *Router) handleShortVideoAlbumImage(w http.ResponseWriter, req *http.Request) {
	store, dir, post, ok := r.svPostFromRequest(w, req)
	if !ok {
		return
	}
	index, err := strconv.Atoi(req.URL.Query().Get("index"))
	if err != nil || post.Kind != shortvideo.KindAlbum || index < 1 || index > post.ImageCount {
		http.Error(w, "invalid image index", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if post.OnDisk {
		full, err := store.DiskAlbumImage(dir, post.ID, index)
		if err != nil {
			http.NotFound(w, req)
			return
		}
		http.ServeFile(w, req, full)
		return
	}
	data, err := store.AlbumImage(post.ID, index)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	_, _ = w.Write(data)
}

// handleShortVideoSequence 返回某频道播放序列中的第 index 条视频（多联放映用）。
// order=shuffle 时按 seed 做确定性洗牌，同一 seed 的序列稳定，客户端可前后切换。
// index 超出范围时按总数取模，便于循环播放。
func (r *Router) handleShortVideoSequence(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	index, err := strconv.Atoi(q.Get("index"))
	if err != nil || index < 0 {
		http.Error(w, "invalid index", http.StatusBadRequest)
		return
	}
	seed, _ := strconv.ParseInt(q.Get("seed"), 10, 64)
	filter := shortvideo.Filter{Platform: q.Get("platform"), CreatorID: q.Get("creator"), Kind: shortvideo.KindVideo, NotLandscape: q.Get("portrait") == "1"}
	if codecs := q.Get("codecs"); codecs != "" {
		filter.Codecs = strings.Split(codecs, ",")
		if len(filter.Codecs) > 16 {
			http.Error(w, "too many codecs", http.StatusBadRequest)
			return
		}
	}
	all, ok := r.svMergedKeys(w, req, filter)
	if !ok {
		return
	}
	likes := r.svLikes(req)
	if q.Get("liked") == "1" {
		all = onlyLiked(all, likes)
	}
	if len(all) == 0 {
		http.Error(w, "no videos in channel", http.StatusNotFound)
		return
	}
	if q.Get("order") == "shuffle" {
		rng := rand.New(rand.NewPCG(uint64(seed), 0x5eed))
		rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	}
	index %= len(all)
	e := all[index]
	post, found, err := e.store.Post(e.ID)
	if err != nil || !found {
		http.Error(w, "video unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(all), "index": index, "post": svPost{Post: post, LibraryID: e.lib, Liked: likes[e.ID]}})
}

// handleShortVideoLike 点赞或取消：POST {"id": 作品ID, "liked": true|false}，需要登录账号。
func (r *Router) handleShortVideoLike(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.UserFromContext(req.Context())
	if user.ID <= 0 {
		http.Error(w, "login required", http.StatusForbidden)
		return
	}
	var in struct {
		ID    string `json:"id"`
		Liked bool   `json:"liked"`
	}
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil || !validPostID(in.ID) {
		http.Error(w, "invalid like", http.StatusBadRequest)
		return
	}
	if err := r.userState.SetShortVideoLike(req.Context(), user.ID, in.ID, in.Liked); err != nil {
		http.Error(w, "like store unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"liked": in.Liked})
}

// svTrash 把一条视频移到系统回收站并从索引中去掉（管理员接口 admin/trash?library_id=&id=）。
func (r *Router) svTrash(w http.ResponseWriter, req *http.Request) {
	store, dir, post, ok := r.svPostFromRequest(w, req)
	if !ok {
		return
	}
	if err := store.TrashPost(dir, post, platform.Trash); err != nil {
		http.Error(w, "move to trash failed: "+err.Error(), http.StatusConflict)
		return
	}
	_ = r.userState.RemoveShortVideoLikes(req.Context(), post.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "trashed"})
}
