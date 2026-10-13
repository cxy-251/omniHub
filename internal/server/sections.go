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
	for _, section := range []string{"gallery", "shortvideo", "text", "audio", "manga", "games", "tools"} {
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
	if u.Role == auth.RoleAdmin {
		allowed = append(allowed, "admin")
		if !auth.IsWANFromContext(req.Context()) {
			allowed = append(allowed, "tasks", "libraries")
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sections": allowed})
}
