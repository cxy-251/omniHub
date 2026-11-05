package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/btmanga"
	"omnihub/internal/tasks"
)

// handleBTManga is a host-only API used by the desktop download page.
func (r *Router) handleBTManga(w http.ResponseWriter, req *http.Request) {
	if !auth.IsTrustedLocalFromContext(req.Context()) {
		http.Error(w, "只能在主机桌面端使用漫画下载", 403)
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.downloads.mu.Lock()
	proxy := r.downloads.settings.YtdlpProxy
	r.downloads.mu.Unlock()
	switch strings.TrimPrefix(req.URL.Path, "/api/downloads/bt/") {
	case "catalog":
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		list, err := btmanga.SearchCatalogSource(req.Context(), req.URL.Query().Get("q"), page, req.URL.Query().Get("source"), proxy)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		r.btCatalog.remember(list.Items)
		writeJSON(w, 200, list)
	case "releases":
		key := req.URL.Query().Get("key")
		if key == "" {
			key = "bangumi:" + req.URL.Query().Get("id")
		}
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		book, cached := r.btCatalog.find(key)
		var err error
		if !cached {
			book, err = btmanga.GetCatalogSubject(req.Context(), key, proxy)
		}
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		category := req.URL.Query().Get("category")
		if category != "zh" && category != "3_1" && category != "3_2" && category != "3_3" {
			category = "3_0"
		}
		list, err := btmanga.SearchReleases(req.Context(), book, category, page, proxy)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		writeJSON(w, 200, list)
	case "cover":
		r.btCover(w, req, proxy)
	case "status":
		tool := btmanga.Tool("aria2c")
		seven := btmanga.Tool("7zz")
		if seven == "" {
			seven = btmanga.Tool("7z")
		}
		writeJSON(w, 200, map[string]any{"available": tool != "", "aria2": tool, "sevenzip": seven, "catalogs": btmanga.CatalogSources(), "source": "Bangumi / MangaDex / Open Library · 动漫花园 / Nyaa / APIBay 漫画资源"})
	case "search":
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		list, err := btmanga.Search(req.Context(), req.URL.Query().Get("q"), req.URL.Query().Get("category"), page, proxy)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		writeJSON(w, 200, map[string]any{"items": list, "page": page})
	default:
		http.NotFound(w, req)
	}
}

// Only retain the latest catalog page: clicking a cover can reuse its validated
// metadata without refetching each provider, and the cache stays bounded.
type btCatalogState struct {
	mu       sync.Mutex
	books    map[string]btmanga.Subject
	loadedAt time.Time
}

func (s *btCatalogState) remember(books []btmanga.Subject) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.books = make(map[string]btmanga.Subject, len(books))
	for _, b := range books {
		s.books[b.Key] = b
	}
	s.loadedAt = time.Now()
}

func (s *btCatalogState) find(key string) (btmanga.Subject, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	book, ok := s.books[key]
	return book, ok && time.Since(s.loadedAt) < 30*time.Minute
}

func (r *Router) btCover(w http.ResponseWriter, req *http.Request, proxy string) {
	address := req.URL.Query().Get("url")
	if !btmanga.ValidCoverURL(address) {
		http.Error(w, "invalid cover", http.StatusBadRequest)
		return
	}
	file := filepath.Join(r.cacheDir, "bt-covers", fmt.Sprintf("%x", sha256.Sum256([]byte(address))))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if _, err := os.Stat(file); err == nil {
		http.ServeFile(w, req, file)
		return
	}
	data, mime, err := btmanga.FetchCover(req.Context(), address, proxy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := cacheBTCover(file, data); err != nil {
		log.Printf("cache comic cover: %v", err)
	}
	w.Header().Set("Content-Type", mime)
	http.ServeContent(w, req, "cover", time.Time{}, bytes.NewReader(data))
}

func cacheBTCover(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".cover-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}

func (r *Router) btKind() *dlKind {
	libs, def := r.writableLibraryChoice("media_library/manga")
	k := &dlKind{ID: "btmanga", Title: "普通漫画 · aria2", Description: "搜索整包资源，选择版本后用外部 aria2 下载；分卷合成一个 CBZ，校验后放进漫画库 standard/。", Fields: []dlField{{Name: "library", Type: "choice", Label: "资源库", Options: libs, Default: def}}, build: func(r *Router, p dlParams) (dlPlan, error) { return r.buildBTManga(p) }}
	if btmanga.Tool("aria2c") == "" {
		k.Missing = []string{"aria2c"}
	}
	return k
}

func (r *Router) buildBTManga(p dlParams) (dlPlan, error) {
	data, err := json.Marshal(p["books"])
	if err != nil || len(data) > 60<<10 {
		return dlPlan{}, errors.New("漫画列表过大")
	}
	var books []btmanga.Book
	if json.Unmarshal(data, &books) != nil || len(books) == 0 || len(books) > 50 {
		return dlPlan{}, errors.New("请加入 1–50 部漫画")
	}
	lib, err := r.writableLibrary(p.text("library"))
	if err != nil {
		return dlPlan{}, err
	}
	root, err := r.libMgr.ResolveCategoryPath(lib.ID+":media_library/manga", "media_library/manga")
	if err != nil {
		return dlPlan{}, err
	}
	work, err := r.libMgr.ResolveCategoryPath(lib.ID+":media_library/manga/.aria2-downloads", "media_library/manga")
	if err != nil {
		return dlPlan{}, err
	}
	dest, err := r.libMgr.ResolveCategoryPath(lib.ID+":media_library/manga/standard", "media_library/manga")
	if err != nil {
		return dlPlan{}, err
	}
	names := map[string]bool{}
	for _, b := range books {
		name, err := btmanga.Filename(b.Title)
		if err != nil {
			return dlPlan{}, err
		}
		if names[strings.ToLower(name)] {
			return dlPlan{}, errors.New("同一本的多个来源请合并到同一项")
		}
		names[strings.ToLower(name)] = true
		if len(b.Sources) == 0 || len(b.Sources) > 30 {
			return dlPlan{}, errors.New("每本需要 1–30 个种子或链接")
		}
		if _, err = os.Lstat(filepath.Join(dest, name)); !errors.Is(err, os.ErrNotExist) {
			return dlPlan{}, fmt.Errorf("%s 已存在或不可访问，不会覆盖", name)
		}
		for _, src := range b.Sources {
			if strings.ContainsAny(src, "\x00\r\n\t") || len(src) > 8192 {
				return dlPlan{}, errors.New("下载来源无效")
			}
			u, e := url.Parse(src)
			if e == nil && ((u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" || u.Scheme == "magnet" && u.Query().Get("xt") != "") {
				continue
			}
			if !filepath.IsAbs(src) || !strings.EqualFold(filepath.Ext(src), ".torrent") {
				return dlPlan{}, errors.New("只接受 HTTP(S)、磁力链接或本机 .torrent 文件")
			}
			st, e := os.Lstat(src)
			if e != nil || !st.Mode().IsRegular() || st.Size() > 8<<20 {
				return dlPlan{}, errors.New("种子文件无效或超过 8 MiB")
			}
		}
	}
	if err = os.MkdirAll(work, 0755); err != nil {
		return dlPlan{}, err
	}
	plan := btmanga.Plan{Aria2: btmanga.Tool("aria2c"), Work: work, Destination: dest, Books: books}
	r.downloads.mu.Lock()
	plan.Proxy = r.downloads.settings.YtdlpProxy
	r.downloads.mu.Unlock()
	f, err := os.CreateTemp(work, "plan-*.json")
	if err != nil {
		return dlPlan{}, err
	}
	if err = errors.Join(json.NewEncoder(f).Encode(plan), f.Close()); err != nil {
		return dlPlan{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return dlPlan{}, err
	}
	return dlPlan{title: fmt.Sprintf("普通漫画 · %d 部", len(books)), lib: lib, resource: root, command: &tasks.Command{Executable: exe, Args: []string{"--bt-manga", f.Name()}, Directory: work}}, nil
}
