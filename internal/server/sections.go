package server

import (
	"net/http"
	"omnihub/internal/auth"
	"strings"
)

var sectionCategories = map[string][]string{
	"gallery": {"media_library/gallery"}, "shortvideo": {"media_library/shortvideo"},
	"audio": {"media_library/audio"}, "manga": {"media_library/manga"},
	"text": {"media_library/docs", "media_library/novels"},
}

func (r *Router) sectionEnabled(section string) bool {
	if section == "games" {
		for _, dir := range r.libMgr.ActiveDirectories() {
			if strings.HasPrefix(dir, "standalone_games/") {
				return true
			}
		}
		return false
	}
	dirs, known := sectionCategories[section]
	if !known {
		return true
	}
	for _, dir := range dirs {
		if !r.libMgr.IsCategoryDisabled(dir) {
			return true
		}
	}
	return false
}

func (r *Router) handleSections(w http.ResponseWriter, req *http.Request) {
	u := auth.UserFromContext(req.Context())
	allowed := make([]string, 0, 8)
	for _, section := range []string{"gallery", "shortvideo", "text", "audio", "manga", "games"} {
		if !r.sectionEnabled(section) {
			continue
		}
		level, err := r.authDB.GetSectionRequiredLevel(section)
		if err != nil {
			http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
			return
		}
		if u.Level >= level {
			allowed = append(allowed, section)
		}
	}
	// 工具：能用至少一个就显示「工具」，并列出能用的工具（tools/<名字>）。
	if r.sectionEnabled("tools") {
		tools, err := r.allowedTools(req)
		if err != nil {
			http.Error(w, "permission store unavailable", http.StatusServiceUnavailable)
			return
		}
		if len(tools) > 0 {
			allowed = append(allowed, "tools")
		}
		for _, t := range tools {
			allowed = append(allowed, "tools/"+t)
		}
	}
	if u.Role == auth.RoleAdmin {
		allowed = append(allowed, "admin")
		if !auth.IsWANFromContext(req.Context()) {
			allowed = append(allowed, "tasks")
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sections": allowed})
}
