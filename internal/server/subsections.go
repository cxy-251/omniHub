package server

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/gallery"
)

// 子分类分级（规格第 5 节的细化）：媒体分类下的一级目录是一个子分类，
// ID 为「大分类/目录」，如 text/novels/nsfw、audio/nsfw。
// 没有单独设置的子分类沿用大分类的等级；名为 nsfw 的目录、说明文件（collection.json）标了 NSFW 的目录默认仅管理员可见。
// 文档的一级目录是主题（C++、Graphic…）而不是分级，所以只有 nsfw 目录单独分级，其余跟随「文本」。
// 短视频的一级目录是平台（shortvideo/X）；相册的子分类是各图库（gallery/<图库>，根目录的相册跟随「相册」），不按路径，见 nsfwMarks。
// rootFiles 不为空时，直接放在分类根目录下的文件算作这个子分类：漫画以前平铺在 manga/ 下、全是 NSFW，
// 现在分成 manga/nsfw/ 与 manga/standard/；可写库会自动搬家（见 migrateMangaLayout），只读库或没搬动的仍按 NSFW 对待。
var subsectionRoots = []struct {
	category, section, prefix string
	nsfwOnly                  bool
	rootFiles                 string
}{
	{"media_library/novels", "text", "text/novels", false, ""},
	{"media_library/docs", "text", "text/docs", true, ""},
	{"media_library/audio", "audio", "audio", false, ""},
	{"media_library/manga", "manga", "manga", false, "nsfw"},
	{shortVideoCategory, "shortvideo", "shortvideo", false, ""},
}

// nsfwMarks 缓存标了 NSFW 的子分类（短视频平台目录、相册图库），30 秒内不重复读说明文件（缩略图请求很密）。
type nsfwMarks struct {
	mu  sync.Mutex
	at  time.Time
	set map[string]bool
}

func (r *Router) markedNSFW() map[string]bool {
	m := &r.nsfwMarks
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set != nil && time.Since(m.at) < 30*time.Second {
		return m.set
	}
	set := map[string]bool{}
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline {
			continue
		}
		if dir, err := r.libMgr.ResolveCategoryPath(lib.ID+":"+shortVideoCategory, shortVideoCategory); err == nil {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && gallery.MarkedNSFW(filepath.Join(dir, e.Name())) {
					set["shortvideo/"+e.Name()] = true
				}
			}
		}
		for _, c := range r.galleryCollections(lib) {
			if c.Key != "" && c.NSFW {
				set["gallery/"+c.Key] = true
			}
		}
	}
	m.set, m.at = set, time.Now()
	return set
}

// subsectionOf 由逻辑路径（<库ID>:media_library/...）得到所属大分类与子分类；不在分级目录内时 sub 为空。
func subsectionOf(logical string) (section, sub string) {
	rel := logical
	if _, after, ok := strings.Cut(logical, ":"); ok {
		rel = after
	}
	for _, root := range subsectionRoots {
		if rest, ok := strings.CutPrefix(rel, root.category+"/"); ok {
			// 一级目录本身（如以目录为单位的音频专辑 audio/nsfw）同样属于该子分类。
			dir, _, nested := strings.Cut(rest, "/")
			if !nested && root.rootFiles != "" {
				dir = root.rootFiles
			}
			if root.nsfwOnly && dir != "nsfw" {
				return root.section, ""
			}
			if root.section == "shortvideo" {
				return root.section, shortVideoPermSub(dir)
			}
			return root.section, root.prefix + "/" + dir
		}
	}
	return "", ""
}

// shortVideoPermSub 将抖音、快手、小红书、TikTok 合并为同一个权限子分区 shortvideo/mainstream。
func shortVideoPermSub(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "抖音", "快手", "小红书", "tiktok":
		return "shortvideo/mainstream"
	default:
		return "shortvideo/" + platform
	}
}

// gamePermSub 将游戏分为「模拟器」（retro / 3ds）与「其他」两个权限子分区。
func gamePermSub(category string) string {
	cat := strings.TrimSuffix(strings.TrimPrefix(category, "standalone_games/"), "_games")
	if cat == "retro" || cat == "3ds" {
		return "games/emulator"
	}
	return "games/other"
}

// accessChecker 在一次请求内缓存各子分类的判定结果。
type accessChecker struct {
	r      *Router
	level  int
	stored map[string]int
	nsfw   map[string]bool // 标了 NSFW 的子分类
	memo   map[string]bool
}

func (r *Router) accessFor(req *http.Request) (*accessChecker, error) {
	stored, err := r.authDB.ListSectionPermissions()
	if err != nil {
		return nil, err
	}
	return &accessChecker{r: r, level: auth.UserFromContext(req.Context()).Level, stored: stored, nsfw: r.markedNSFW(), memo: map[string]bool{}}, nil
}

// requiredLevel 返回子分类的最低等级：单独设置 > nsfw 目录、标了 NSFW 的默认管理员 > 大分类等级。
func (c *accessChecker) requiredLevel(section, sub string) int {
	if lvl, ok := c.stored[sub]; ok && sub != "" {
		return lvl
	}
	if sub == "shortvideo/mainstream" {
		for _, legacy := range []string{"shortvideo/抖音", "shortvideo/快手", "shortvideo/小红书", "shortvideo/TikTok"} {
			if lvl, ok := c.stored[legacy]; ok {
				return lvl
			}
		}
	}
	if sub != "" && (path.Base(sub) == "nsfw" || c.nsfw[sub]) {
		return auth.LevelAdmin
	}
	return c.stored[section]
}

// allows 判断当前账号能否访问该逻辑路径。
func (c *accessChecker) allows(logical string) bool {
	section, sub := subsectionOf(logical)
	if section == "" {
		return true
	}
	return c.allowsSub(section, sub)
}

// allowsSub 判断当前账号能否访问大分类下的子分类（sub 为空时只看大分类）。
func (c *accessChecker) allowsSub(section, sub string) bool {
	key := section + "|" + sub
	ok, cached := c.memo[key]
	if !cached {
		ok = c.level >= c.requiredLevel(section, sub)
		c.memo[key] = ok
	}
	return ok
}

// shortVideo 判断能否访问短视频作品（ID 是 平台/博主/作品，按平台分级）。
func (c *accessChecker) shortVideo(id string) bool {
	platform, _, _ := strings.Cut(id, "/")
	return c.allowsSub("shortvideo", shortVideoPermSub(platform))
}

// gameCategory 判断能否访问某类游戏（retro/3ds 归入模拟器 games/emulator，其余归入 games/other）。
func (c *accessChecker) gameCategory(category string) bool {
	return c.allowsSub("games", gamePermSub(category))
}

// galleryCollection 判断能否访问相册的图库（key 为空是根目录的默认相册，对应子分类 gallery/default，未单独设置时沿用「相册」）。
func (c *accessChecker) galleryCollection(key string) bool {
	if key == "" {
		return c.allowsSub("gallery", "gallery/default")
	}
	return c.allowsSub("gallery", "gallery/"+key)
}

// allowPath 校验单个资源的子分类等级，不足时写 403 并返回 false。
func (r *Router) allowPath(w http.ResponseWriter, req *http.Request, logical string) bool {
	c, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
		return false
	}
	if !c.allows(logical) {
		http.Error(w, "forbidden: insufficient permission level", http.StatusForbidden)
		return false
	}
	return true
}

var weblabSubsections = []string{"rubiks", "fluid", "fourier", "slides"}

// handleAdminSubsections 列出各在线库中实际存在的子分类及其生效等级，供权限矩阵展示与修改
// （修改仍走 /api/admin/permissions，section_id 填子分类 ID）。
func (r *Router) handleAdminSubsections(w http.ResponseWriter, req *http.Request) {
	if auth.UserFromContext(req.Context()).Role != auth.RoleAdmin {
		http.Error(w, "forbidden: admin required", http.StatusForbidden)
		return
	}
	c, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
		return
	}
	type entry struct {
		ID            string `json:"id"`
		Section       string `json:"section"`
		RequiredLevel int    `json:"required_level"`
		Explicit      bool   `json:"explicit"`
	}
	seen := map[string]entry{}
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline {
			continue
		}
		for _, root := range subsectionRoots {
			dir, err := r.libMgr.ResolveCategoryPath(lib.ID+":"+root.category, root.category)
			if err != nil {
				continue
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || (root.nsfwOnly && e.Name() != "nsfw") {
					continue
				}
				id := root.prefix + "/" + e.Name()
				if root.section == "shortvideo" {
					id = shortVideoPermSub(e.Name())
				}
				_, explicit := c.stored[id]
				seen[id] = entry{ID: id, Section: root.section, RequiredLevel: c.requiredLevel(root.section, id), Explicit: explicit}
			}
		}
	}
	for _, lib := range r.libMgr.ListLibraries() {
		if !lib.IsOnline {
			continue
		}
		for _, coll := range r.galleryCollections(lib) {
			subKey := coll.Key
			if subKey == "" {
				subKey = "default"
			}
			id := "gallery/" + subKey
			_, explicit := c.stored[id]
			seen[id] = entry{ID: id, Section: "gallery", RequiredLevel: c.requiredLevel("gallery", id), Explicit: explicit}
		}
	}
	for _, id := range []string{"games/emulator", "games/other"} {
		_, explicit := c.stored[id]
		seen[id] = entry{ID: id, Section: "games", RequiredLevel: c.requiredLevel("games", id), Explicit: explicit}
	}
	for _, sub := range weblabSubsections {
		id := "weblab/" + sub
		_, explicit := c.stored[id]
		seen[id] = entry{ID: id, Section: "weblab", RequiredLevel: c.requiredLevel("weblab", id), Explicit: explicit}
	}
	for _, t := range toolDefaults {
		id := "tools/" + t.name
		level, explicit := c.stored[id]
		if !explicit {
			level = t.level
		}
		seen[id] = entry{ID: id, Section: "tools", RequiredLevel: level, Explicit: explicit}
	}
	out := make([]entry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, out)
}
