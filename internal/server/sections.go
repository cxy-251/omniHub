package server

import (
	"net/http"
	"omnihub/internal/auth"
)

func (r *Router) handleSections(w http.ResponseWriter, req *http.Request) {
	u := auth.UserFromContext(req.Context())
	allowed := make([]string, 0, 8)
	for _, section := range []string{"gallery", "shortvideo", "text", "audio", "manga", "games", "tools"} {
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
			allowed = append(allowed, "tasks")
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sections": allowed})
}
