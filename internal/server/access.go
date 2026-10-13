package server

import (
	"net/http"
	"net/url"
	"strings"

	"omnihub/internal/auth"
)

// protectRoutes applies section permissions to every resource endpoint, including binary streams.
func (r *Router) protectRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodOptions {
			if origin := req.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != req.Host || (u.Scheme != "http" && u.Scheme != "https") {
					http.Error(w, "cross-origin state change forbidden", http.StatusForbidden)
					return
				}
			}
		}
		// Upload streams retain their own bounded chunk limit. JSON control payloads are small.
		if !strings.HasPrefix(req.URL.Path, "/api/tools/shared/upload") {
			req.Body = http.MaxBytesReader(w, req.Body, 64*1024)
		}
		path := req.URL.Path
		for _, section := range []string{"gallery", "shortvideo", "text", "audio", "manga"} {
			if strings.HasPrefix(path, "/api/media/"+section+"/") && !r.checkSectionAccess(w, req, section, auth.TargetAll) {
				return
			}
		}
		if (strings.HasPrefix(path, "/api/games/") || strings.HasPrefix(path, "/standalone_games/")) && !r.checkSectionAccess(w, req, "games", auth.TargetAll) {
			return
		}
		if strings.HasPrefix(path, "/api/tasks/") || path == "/api/tools/webshell" || strings.HasPrefix(path, "/api/sc2/") {
			if auth.IsWANFromContext(req.Context()) || auth.UserFromContext(req.Context()).Role != auth.RoleAdmin {
				http.Error(w, "trusted environment administrator required", http.StatusForbidden)
				return
			}
		}
		if strings.HasPrefix(path, "/api/mega/") && !auth.IsTrustedLocalFromContext(req.Context()) {
			http.Error(w, "trusted local client required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, req)
	})
}
