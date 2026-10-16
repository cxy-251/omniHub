package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/feed"
	"omnihub/internal/library"
	"omnihub/internal/platform"
	"omnihub/internal/shortvideo"
)

// 推荐、点赞、行为记录、固定分组、回收站、多联放映序列：短视频与相册共用的接口 /api/feed/{domain}/...
// 每种内容只实现 feedSource：列出全部条目（带特征标签）、把一页条目渲染成 JSON、移到回收站。
//
// 频道 channel：recommend 推荐 | liked 我的点赞 | hidden 回收站 | group:<分组> | tag:<标签> | 空 = 全部（时间倒序）。
// 筛选：portrait=1 只要竖屏（多联放映），kind=<类型>（短视频的 video / album）。

type feedSource interface {
	// snapshotKey 标识当前数据版本（数据库文件修改时间等），变化时重建条目列表。
	snapshotKey(req *http.Request) (string, error)
	items(req *http.Request) ([]feed.Item, error)
	render(req *http.Request, items []feed.Item, liked map[string]bool) (any, error)
	groups(req *http.Request, items []feed.Item) any
	trash(req *http.Request, it feed.Item) error
}

type feedSnapshot struct {
	key   string
	items []feed.Item
	index map[string]int
	used  time.Time
}

type feedOrder struct {
	key   string
	order []int
}

type feedState struct {
	mu        sync.Mutex
	snapshots map[string]*feedSnapshot // domain → 条目列表
	orders    []feedOrder              // 最近的排序结果（翻页、多联放映取片时复用）
}

func (r *Router) feedSources() map[string]feedSource {
	return map[string]feedSource{"shortvideo": svFeed{r}, "gallery": galleryFeed{r}}
}

// snapshot 返回某类内容的全部条目；10 分钟没人用就释放（相册 10 万条约十几 MB）。
func (r *Router) feedSnapshot(req *http.Request, domain string, src feedSource) (*feedSnapshot, error) {
	key, err := src.snapshotKey(req)
	if err != nil {
		return nil, err
	}
	r.feed.mu.Lock()
	if r.feed.snapshots == nil {
		r.feed.snapshots = map[string]*feedSnapshot{}
	}
	for d, s := range r.feed.snapshots {
		if time.Since(s.used) > 10*time.Minute {
			delete(r.feed.snapshots, d)
		}
	}
	if s := r.feed.snapshots[domain]; s != nil && s.key == key {
		s.used = time.Now()
		r.feed.mu.Unlock()
		return s, nil
	}
	r.feed.mu.Unlock()
	items, err := src.items(req)
	if err != nil {
		return nil, err
	}
	s := &feedSnapshot{key: key, items: items, index: make(map[string]int, len(items)), used: time.Now()}
	for i, it := range items {
		s.index[it.ID] = i
	}
	r.feed.mu.Lock()
	r.feed.snapshots[domain] = s
	r.feed.mu.Unlock()
	return s, nil
}

type feedQuery struct {
	channel  string
	seed     int64
	portrait bool
	kind     string
	shuffle  bool
}

func parseFeedQuery(req *http.Request) feedQuery {
	q := req.URL.Query()
	seed, _ := strconv.ParseInt(q.Get("seed"), 10, 64)
	return feedQuery{channel: q.Get("channel"), seed: seed, portrait: q.Get("portrait") == "1", kind: q.Get("kind"), shuffle: q.Get("order") == "shuffle"}
}

func hasTag(it *feed.Item, tag string) bool {
	for _, t := range it.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// feedOrderFor 返回频道里的条目顺序（snapshot.items 的下标）。推荐结果按种子缓存，翻页时顺序不变。
func (r *Router) feedOrderFor(req *http.Request, domain string, snap *feedSnapshot, q feedQuery) ([]int, error) {
	ctx := req.Context()
	user := auth.UserFromContext(ctx)
	cacheKey := fmt.Sprintf("%d|%s|%s|%s|%d|%t|%s|%t", user.ID, domain, snap.key, q.channel, q.seed, q.portrait, q.kind, q.shuffle)
	r.feed.mu.Lock()
	for _, o := range r.feed.orders {
		if o.key == cacheKey {
			r.feed.mu.Unlock()
			return o.order, nil
		}
	}
	r.feed.mu.Unlock()

	hidden, err := r.userState.Hidden(ctx, domain)
	if err != nil {
		return nil, err
	}
	keep := func(it *feed.Item) bool {
		if q.portrait && it.Ratio > 0 && it.Ratio <= 1000 { // 竖屏：高大于宽（方形也排除）
			return false
		}
		if q.kind != "" && !hasTag(it, "kind:"+q.kind) {
			return false
		}
		return true
	}
	var order []int
	switch {
	case q.channel == "hidden":
		for i := range snap.items {
			if _, ok := hidden[snap.items[i].ID]; ok {
				order = append(order, i)
			}
		}
	case q.channel == "liked":
		likes, err := r.userState.Likes(ctx, user.ID, domain)
		if err != nil {
			return nil, err
		}
		for _, id := range likes {
			if i, ok := snap.index[id]; ok && keep(&snap.items[i]) {
				if _, h := hidden[id]; !h {
					order = append(order, i)
				}
			}
		}
	default:
		for i := range snap.items {
			it := &snap.items[i]
			if _, h := hidden[it.ID]; h || !keep(it) {
				continue
			}
			if g, ok := strings.CutPrefix(q.channel, "group:"); ok && it.Group != g {
				continue
			}
			if t, ok := strings.CutPrefix(q.channel, "tag:"); ok && !hasTag(it, t) {
				continue
			}
			order = append(order, i)
		}
		if q.channel == "recommend" {
			sig, err := r.feedSignals(ctx, user.ID, domain)
			if err != nil {
				return nil, err
			}
			subset := make([]feed.Item, len(order))
			for k, i := range order {
				subset[k] = snap.items[i]
			}
			ranked := feed.Rank(subset, sig, q.seed, time.Now())
			for k, j := range ranked {
				ranked[k] = order[j]
			}
			order = ranked
		}
	}
	if q.shuffle && q.channel != "recommend" {
		rng := rand.New(rand.NewPCG(uint64(q.seed), 0x5eed))
		rng.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })
	}
	r.feed.mu.Lock()
	r.feed.orders = append(r.feed.orders, feedOrder{cacheKey, order})
	if len(r.feed.orders) > 8 {
		r.feed.orders = r.feed.orders[1:]
	}
	r.feed.mu.Unlock()
	return order, nil
}

func (r *Router) feedSignals(ctx context.Context, userID int64, domain string) (feed.Signals, error) {
	s := feed.Signals{Liked: map[string]bool{}, Seen: map[string]feed.Seen{}, Pinned: map[string]bool{}}
	var err error
	if s.Profile, err = r.userState.Profile(ctx, userID, domain); err != nil {
		return s, err
	}
	likes, err := r.userState.Likes(ctx, userID, domain)
	if err != nil {
		return s, err
	}
	for _, id := range likes {
		s.Liked[id] = true
	}
	seen, err := r.userState.SeenItems(ctx, userID, domain)
	if err != nil {
		return s, err
	}
	for id, v := range seen {
		s.Seen[id] = feed.Seen{Count: int(v[0]), Last: v[1]}
	}
	pins, err := r.userState.Pins(ctx, userID, domain)
	if err != nil {
		return s, err
	}
	for _, g := range pins {
		s.Pinned[g] = true
	}
	return s, nil
}

func (r *Router) likedSet(ctx context.Context, domain string) map[string]bool {
	out := map[string]bool{}
	if likes, err := r.userState.Likes(ctx, auth.UserFromContext(ctx).ID, domain); err == nil {
		for _, id := range likes {
			out[id] = true
		}
	}
	return out
}

// 行为对偏好的影响（加到条目的每个特征标签上）。
var feedDeltas = map[string]float64{"complete": 1, "dwell": 0.3, "skip": -0.6, "like": 3, "unlike": -3, "trash": -3}

// handleFeed 处理 /api/feed/{domain}/{action}。
func (r *Router) handleFeed(w http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, "/api/feed/")
	domain, action, _ := strings.Cut(rest, "/")
	src := r.feedSources()[domain]
	if src == nil {
		http.NotFound(w, req)
		return
	}
	section := map[string]string{"shortvideo": "shortvideo", "gallery": "gallery"}[domain]
	if !r.checkSectionAccess(w, req, section, auth.TargetAll) {
		return
	}
	snap, err := r.feedSnapshot(req, domain, src)
	if err != nil {
		http.Error(w, "index unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	ctx := req.Context()
	user := auth.UserFromContext(ctx)
	switch action {
	case "items", "ratios", "sequence":
		q := parseFeedQuery(req)
		if q.channel == "hidden" && (user.Role != auth.RoleAdmin || auth.IsWANFromContext(ctx)) {
			http.Error(w, "admin required", http.StatusForbidden)
			return
		}
		order, err := r.feedOrderFor(req, domain, snap, q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		switch action {
		case "ratios":
			ratios := make([]int, len(order))
			for k, i := range order {
				ratios[k] = snap.items[i].Ratio
			}
			writeJSON(w, http.StatusOK, map[string]any{"ratios": ratios})
		case "items":
			limit, offset, err := pageBounds(req)
			if err != nil || limit > 200 {
				http.Error(w, "invalid page bounds", http.StatusBadRequest)
				return
			}
			w.Header().Set("X-Total-Count", strconv.Itoa(len(order)))
			page := order[min(offset, len(order)):min(offset+limit, len(order))]
			items := make([]feed.Item, len(page))
			for k, i := range page {
				items[k] = snap.items[i]
			}
			out, err := src.render(req, items, r.likedSet(ctx, domain))
			if err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			writeJSON(w, http.StatusOK, out)
		case "sequence":
			// 多联放映：第 index 条（超出时取模，循环播放）。
			if len(order) == 0 {
				http.Error(w, "频道里没有内容", http.StatusNotFound)
				return
			}
			index, _ := strconv.Atoi(req.URL.Query().Get("index"))
			index = ((index % len(order)) + len(order)) % len(order)
			out, err := src.render(req, []feed.Item{snap.items[order[index]]}, r.likedSet(ctx, domain))
			if err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			list, _ := out.([]any)
			if len(list) == 0 {
				http.Error(w, "item unavailable", http.StatusServiceUnavailable)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"total": len(order), "index": index, "item": list[0]})
		}
	case "groups":
		pins, _ := r.userState.Pins(ctx, user.ID, domain)
		writeJSON(w, http.StatusOK, map[string]any{"groups": src.groups(req, snap.items), "pins": pins})
	case "like", "event", "pin", "trash", "restore":
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in struct {
			ID     string `json:"id"`
			Liked  bool   `json:"liked"`
			Type   string `json:"type"`
			Group  string `json:"group"`
			Pinned bool   `json:"pinned"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 8<<10)).Decode(&in); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if action == "pin" {
			if user.ID <= 0 || in.Group == "" {
				http.Error(w, "login required", http.StatusForbidden)
				return
			}
			if err := r.userState.SetPin(ctx, user.ID, domain, in.Group, in.Pinned); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			pins, _ := r.userState.Pins(ctx, user.ID, domain)
			writeJSON(w, http.StatusOK, pins)
			return
		}
		i, ok := snap.index[in.ID]
		if !ok {
			http.Error(w, "unknown item", http.StatusNotFound)
			return
		}
		it := snap.items[i]
		switch action {
		case "like":
			if user.ID <= 0 {
				http.Error(w, "login required", http.StatusForbidden)
				return
			}
			if err := r.userState.SetLike(ctx, user.ID, domain, it.ID, in.Liked); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			delta := feedDeltas["like"]
			if !in.Liked {
				delta = feedDeltas["unlike"]
			}
			_ = r.userState.AdjustProfile(ctx, user.ID, domain, it.Tags, delta)
			writeJSON(w, http.StatusOK, map[string]bool{"liked": in.Liked})
		case "event":
			// view：展示过（记为看过）；complete / dwell / skip：看完、停留较久、很快划走（计入偏好）。
			delta, known := feedDeltas[in.Type]
			if !known && in.Type != "view" {
				http.Error(w, "unknown event", http.StatusBadRequest)
				return
			}
			if user.ID > 0 {
				_ = r.userState.MarkSeen(ctx, user.ID, domain, it.ID)
				if known && in.Type != "like" && in.Type != "unlike" && in.Type != "trash" {
					_ = r.userState.AdjustProfile(ctx, user.ID, domain, it.Tags, delta)
				}
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "trash", "restore":
			if user.Role != auth.RoleAdmin || auth.IsWANFromContext(ctx) {
				http.Error(w, "admin required", http.StatusForbidden)
				return
			}
			if action == "restore" {
				err = r.userState.SetHidden(ctx, domain, it.ID, it.Lib, false)
			} else if err = src.trash(req, it); err == nil {
				_ = r.userState.AdjustProfile(ctx, user.ID, domain, it.Tags, feedDeltas["trash"])
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			r.dropFeedOrders()
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	default:
		http.NotFound(w, req)
	}
}

// dropFeedOrders 在条目被移走后清掉缓存的排序（序号要重新算）。
func (r *Router) dropFeedOrders() {
	r.feed.mu.Lock()
	r.feed.orders = nil
	r.feed.mu.Unlock()
}

// fileStamp 是文件的修改时间与大小（数据版本标识用）。
func fileStamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 36) + "/" + strconv.FormatInt(info.Size(), 36)
}

// --- 短视频 ---

type svFeed struct{ r *Router }

func (s svFeed) snapshotKey(req *http.Request) (string, error) {
	libs, err := s.r.onlineLibraries()
	if err != nil {
		return "", err
	}
	key := ""
	for _, lib := range libs {
		dir, err := s.r.moduleDirectory(lib.RootPath, shortVideoCategory)
		if err != nil {
			continue
		}
		key += lib.ID + ":" + fileStamp(filepath.Join(dir, "index.db")) + fileStamp(filepath.Join(dir, "index.db-wal")) + "|"
	}
	return key, nil
}

func durationTag(d float64) string {
	switch {
	case d <= 0:
		return ""
	case d < 15:
		return "dur:short"
	case d < 60:
		return "dur:mid"
	default:
		return "dur:long"
	}
}

func (s svFeed) items(req *http.Request) ([]feed.Item, error) {
	libs, err := s.r.onlineLibraries()
	if err != nil {
		return nil, err
	}
	var out []feed.Item
	for _, lib := range libs {
		store, _, err := s.r.svStore(lib)
		if err != nil {
			continue
		}
		keys, err := store.Keys(shortvideoAll)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			parts := strings.SplitN(k.ID, "/", 3)
			if len(parts) != 3 {
				continue
			}
			creator := parts[0] + "/" + parts[1]
			tags := []string{"creator:" + creator, "platform:" + parts[0], "kind:" + string(k.Kind)}
			if t := durationTag(k.Duration); t != "" {
				tags = append(tags, t)
			}
			it := feed.Item{ID: k.ID, Lib: lib.ID, Group: creator, Tags: tags}
			if t, err := time.Parse("2006-01-02", k.Published); err == nil {
				it.Date = t.Unix()
			}
			if k.Width > 0 && k.Height > 0 {
				it.Ratio = k.Height * 1000 / k.Width
			}
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Date > out[b].Date })
	return out, nil
}

func (s svFeed) render(req *http.Request, items []feed.Item, liked map[string]bool) (any, error) {
	byLib := map[string][]string{}
	for _, it := range items {
		byLib[it.Lib] = append(byLib[it.Lib], it.ID)
	}
	found := map[string]svPost{}
	libs, err := s.r.onlineLibraries()
	if err != nil {
		return nil, err
	}
	for _, lib := range libs {
		ids := byLib[lib.ID]
		if len(ids) == 0 {
			continue
		}
		store, _, err := s.r.svStore(lib)
		if err != nil {
			continue
		}
		posts, err := store.PostsByID(ids)
		if err != nil {
			return nil, err
		}
		for _, p := range posts {
			found[p.ID] = svPost{Post: p, LibraryID: lib.ID, Liked: liked[p.ID]}
		}
	}
	out := make([]any, 0, len(items))
	for _, it := range items {
		if p, ok := found[it.ID]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s svFeed) groups(req *http.Request, items []feed.Item) any {
	type group struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Platform string `json:"platform"`
		Count    int    `json:"count"`
	}
	counts := map[string]int{}
	var ids []string
	for _, it := range items {
		if counts[it.Group] == 0 {
			ids = append(ids, it.Group)
		}
		counts[it.Group]++
	}
	sort.Strings(ids)
	out := make([]group, 0, len(ids))
	for _, id := range ids {
		platform, name, _ := strings.Cut(id, "/")
		out = append(out, group{ID: id, Label: name, Platform: platform, Count: counts[id]})
	}
	return out
}

func (s svFeed) trash(req *http.Request, it feed.Item) error {
	for _, lib := range s.r.libMgr.ListLibraries() {
		if lib.ID != it.Lib {
			continue
		}
		store, dir, err := s.r.svStore(lib)
		if err != nil {
			return err
		}
		post, ok, err := store.Post(it.ID)
		if err != nil || !ok {
			return errors.New("作品不存在")
		}
		if err := store.TrashPost(dir, post, trashFile); err != nil {
			return err
		}
		_ = s.r.userState.RemoveLikes(req.Context(), "shortvideo", it.ID)
		return nil
	}
	return errors.New("资源库不在线")
}

// --- 相册（只读）：回收站只是隐藏，不改 gallery.db ---

type galleryFeed struct{ r *Router }

var colorNames = []string{"红", "橙", "黄", "绿", "蓝", "紫", "粉", "白", "黑", "灰"}

func (g galleryFeed) snapshotKey(req *http.Request) (string, error) {
	libs, err := g.r.onlineLibraries()
	if err != nil {
		return "", err
	}
	key := ""
	for _, lib := range libs {
		dir, err := g.r.moduleDirectory(lib.RootPath, "media_library/gallery", "gallery.db")
		if err != nil {
			continue
		}
		key += lib.ID + ":" + fileStamp(filepath.Join(dir, "gallery.db")) + fileStamp(filepath.Join(dir, "gallery.db-wal")) + "|"
	}
	return key, nil
}

// hexTags 由主色算出色调（12 档，低饱和为 gray）与明暗（4 档）标签。
func hexTags(hex string) []string {
	var r, gr, b int
	if _, err := fmt.Sscanf(strings.TrimPrefix(hex, "#"), "%02x%02x%02x", &r, &gr, &b); err != nil {
		return nil
	}
	rf, gf, bf := float64(r)/255, float64(gr)/255, float64(b)/255
	mx, mn := math.Max(rf, math.Max(gf, bf)), math.Min(rf, math.Min(gf, bf))
	light := fmt.Sprintf("light:%d", min(3, int(mx*4)))
	if mx == 0 || (mx-mn)/mx < 0.15 {
		return []string{"hue:gray", light}
	}
	var h float64
	switch mx {
	case rf:
		h = math.Mod((gf-bf)/(mx-mn), 6)
	case gf:
		h = (bf-rf)/(mx-mn) + 2
	default:
		h = (rf-gf)/(mx-mn) + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return []string{fmt.Sprintf("hue:%d", int(h/30)%12), light}
}

func (g galleryFeed) items(req *http.Request) ([]feed.Item, error) {
	libs, err := g.r.onlineLibraries()
	if err != nil {
		return nil, err
	}
	var out []feed.Item
	for _, lib := range libs {
		dir, err := g.r.moduleDirectory(lib.RootPath, "media_library/gallery", "gallery.db")
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "gallery.db")); err != nil {
			continue
		}
		db, err := g.r.getMetaDB(lib.RootPath)
		if err != nil {
			return nil, err
		}
		rows, err := db.FeedRows()
		if err != nil {
			return nil, err
		}
		for _, p := range rows {
			ratio := 1000
			if p.Width > 0 && p.Height > 0 {
				ratio = p.Height * 1000 / p.Width
			}
			shape := "shape:square"
			if ratio > 1100 {
				shape = "shape:portrait"
			} else if ratio < 900 {
				shape = "shape:landscape"
			}
			color := fmt.Sprintf("color:%d", p.ColorFamily)
			tags := append([]string{color, shape}, hexTags(p.ColorHex)...)
			out = append(out, feed.Item{ID: p.ID, Lib: lib.ID, Group: color, Tags: tags, Ratio: ratio})
		}
	}
	return out, nil
}

func (g galleryFeed) render(req *http.Request, items []feed.Item, liked map[string]bool) (any, error) {
	type likedPhoto struct {
		photoItem
		Liked bool `json:"liked"`
	}
	libs, err := g.r.onlineLibraries()
	if err != nil {
		return nil, err
	}
	byID := map[string]any{}
	for _, lib := range libs {
		var ids []string
		for _, it := range items {
			if it.Lib == lib.ID {
				ids = append(ids, it.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		db, err := g.r.getMetaDB(lib.RootPath)
		if err != nil {
			return nil, err
		}
		photos, err := db.PhotosByID(ids)
		if err != nil {
			return nil, err
		}
		for _, p := range photos {
			byID[p.ID] = likedPhoto{photoItem{Photo: p, LibraryID: lib.ID, LibraryName: lib.Name, ReadOnly: true}, liked[p.ID]}
		}
	}
	out := make([]any, 0, len(items))
	for _, it := range items {
		if p, ok := byID[it.ID]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (g galleryFeed) groups(req *http.Request, items []feed.Item) any {
	type group struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Count int    `json:"count"`
	}
	counts := make([]int, len(colorNames))
	for _, it := range items {
		var c int
		if _, err := fmt.Sscanf(it.Group, "color:%d", &c); err == nil && c >= 0 && c < len(counts) {
			counts[c]++
		}
	}
	out := []group{}
	for c, n := range counts {
		if n > 0 {
			out = append(out, group{ID: fmt.Sprintf("color:%d", c), Label: colorNames[c], Count: n})
		}
	}
	return out
}

func (g galleryFeed) trash(req *http.Request, it feed.Item) error {
	return g.r.userState.SetHidden(req.Context(), "gallery", it.ID, it.Lib, true)
}

// onlineLibraries 返回在线的资源库。
func (r *Router) onlineLibraries() ([]*library.Library, error) {
	var out []*library.Library
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.IsOnline {
			out = append(out, lib)
		}
	}
	return out, nil
}

var (
	shortvideoAll = shortvideo.Filter{}
	trashFile     = platform.Trash
)
