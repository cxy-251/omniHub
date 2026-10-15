package server

import (
	"errors"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"omnihub/internal/library"
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
}

// handleShortVideoPosts 合并各库作品，按发布日期倒序分页。
func (r *Router) handleShortVideoPosts(w http.ResponseWriter, req *http.Request) {
	limit, offset, err := pageBounds(req)
	if err != nil || limit > 500 || offset > 100000 {
		http.Error(w, "invalid page bounds", http.StatusBadRequest)
		return
	}
	q := req.URL.Query()
	filter := shortvideo.Filter{Platform: q.Get("platform"), CreatorID: q.Get("creator"), Kind: shortvideo.Kind(q.Get("kind"))}
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	var all []svPost
	for _, lib := range libs {
		store, _, err := r.svStore(lib)
		if err != nil {
			continue // 未建索引的库（例如只读且没有数据库）不参与
		}
		posts, err := store.Posts(filter, offset+limit+1, 0)
		if err != nil {
			http.Error(w, "short video index unavailable", http.StatusServiceUnavailable)
			return
		}
		for _, p := range posts {
			all = append(all, svPost{Post: p, LibraryID: lib.ID})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Published != all[j].Published {
			return all[i].Published > all[j].Published
		}
		return all[i].ID > all[j].ID
	})
	if offset >= len(all) {
		all = nil
	} else {
		all = all[offset:]
	}
	more := len(all) > limit
	if more {
		all = all[:limit]
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(more))
	writeJSON(w, http.StatusOK, all)
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
// 兼容多联放映使用的 path=<库ID>:media_library/shortvideo/... 逻辑路径。
func (r *Router) handleShortVideoStream(w http.ResponseWriter, req *http.Request) {
	if logical := req.URL.Query().Get("path"); logical != "" {
		full, err := r.libMgr.ResolveCategoryPath(logical, shortVideoCategory)
		if err != nil || strings.ToLower(path.Ext(full)) != ".mp4" {
			http.NotFound(w, req)
			return
		}
		if err := shortvideo.StreamVideo(w, req, full, "video/mp4"); err != nil {
			http.Error(w, "video file unavailable", http.StatusServiceUnavailable)
		}
		return
	}
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
