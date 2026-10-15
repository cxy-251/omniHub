package server

import (
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"omnihub/internal/auth"
)

// 子分类分级（规格第 5 节的细化）：媒体分类下的一级目录是一个子分类，
// ID 为「大分类/目录」，如 text/novels/nsfw、audio/nsfw。
// 没有单独设置的子分类沿用大分类的等级；名为 nsfw 的目录默认仅管理员可见。
var subsectionRoots = []struct {
	category, section, prefix string
}{
	{"media_library/novels", "text", "text/novels"},
	{"media_library/docs", "text", "text/docs"},
	{"media_library/audio", "audio", "audio"},
	{"media_library/manga", "manga", "manga"},
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
			dir, _, _ := strings.Cut(rest, "/")
			return root.section, root.prefix + "/" + dir
		}
	}
	return "", ""
}

// accessChecker 在一次请求内缓存各子分类的判定结果。
type accessChecker struct {
	r      *Router
	level  int
	stored map[string]int
	memo   map[string]bool
}

func (r *Router) accessFor(req *http.Request) (*accessChecker, error) {
	stored, err := r.authDB.ListSectionPermissions()
	if err != nil {
		return nil, err
	}
	return &accessChecker{r: r, level: auth.UserFromContext(req.Context()).Level, stored: stored, memo: map[string]bool{}}, nil
}

// requiredLevel 返回子分类的最低等级：单独设置 > nsfw 默认管理员 > 大分类等级。
func (c *accessChecker) requiredLevel(section, sub string) int {
	if lvl, ok := c.stored[sub]; ok && sub != "" {
		return lvl
	}
	if sub != "" && path.Base(sub) == "nsfw" {
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
	key := section + "|" + sub
	ok, cached := c.memo[key]
	if !cached {
		ok = c.level >= c.requiredLevel(section, sub)
		c.memo[key] = ok
	}
	return ok
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
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				id := root.prefix + "/" + e.Name()
				_, explicit := c.stored[id]
				seen[id] = entry{ID: id, Section: root.section, RequiredLevel: c.requiredLevel(root.section, id), Explicit: explicit}
			}
		}
	}
	out := make([]entry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, out)
}
