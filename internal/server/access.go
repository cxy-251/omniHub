package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"omnihub/internal/auth"
)

// protectRoutes applies section permissions to every resource endpoint, including binary streams.
func (r *Router) protectRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// 网页游戏存档（/game-content/<token>/__save）由沙箱里的游戏页发出，来源是 "null"；
		// 它只凭地址里的授权 token，不用登录凭证，不受跨站伪造影响，所以不做同源检查。
		gameSave := req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/game-content/") && (strings.HasSuffix(req.URL.Path, "/__save") || strings.Contains(req.URL.Path, "/__savefile/"))
		if req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodOptions && !gameSave {
			if origin := req.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != req.Host || (u.Scheme != "http" && u.Scheme != "https") {
					http.Error(w, "cross-origin state change forbidden", http.StatusForbidden)
					return
				}
			}
		}
		// Upload streams, game saves and WebDAV file bodies retain their own limits. JSON control payloads are small.
		if !strings.HasPrefix(req.URL.Path, "/api/tools/shared/upload") && !gameSave && req.URL.Path != davPrefix && !strings.HasPrefix(req.URL.Path, davPrefix+"/") {
			req.Body = http.MaxBytesReader(w, req.Body, 64*1024)
		}
		path := req.URL.Path
		if req.Method == http.MethodPost && strings.HasPrefix(path, "/api/media/") {
			if expected := req.Header.Get("X-Omni-Account-ID"); expected != "" {
				id, err := strconv.ParseInt(expected, 10, 64)
				if err != nil || id <= 0 || id != auth.UserFromContext(req.Context()).ID {
					http.Error(w, "account changed; reopen media before saving", 409)
					return
				}
			}
		}
		for _, section := range []string{"gallery", "shortvideo", "text", "audio", "manga"} {
			if strings.HasPrefix(path, "/api/media/"+section+"/") && !r.sectionEnabled(section) {
				http.Error(w, "category disabled", http.StatusNotFound)
				return
			}
			if strings.HasPrefix(path, "/api/media/"+section+"/") && !r.checkSectionAccess(w, req, section, auth.TargetAll) {
				return
			}
		}
		if (strings.HasPrefix(path, "/api/games/") || strings.HasPrefix(path, "/standalone_games/")) && !r.checkSectionAccess(w, req, "games", auth.TargetAll) {
			return
		}
		if (strings.HasPrefix(path, "/api/games/") || strings.HasPrefix(path, "/standalone_games/")) && !r.sectionEnabled("games") {
			http.Error(w, "category disabled", http.StatusNotFound)
			return
		}
		if strings.HasPrefix(path, "/api/tools/shared/") && r.libMgr.IsCategoryDisabled("media_library/shared") {
			http.Error(w, "category disabled", http.StatusNotFound)
			return
		}
		if strings.HasPrefix(path, "/api/tasks/") || path == "/api/tools/webshell" || strings.HasPrefix(path, "/api/sc2/") || strings.HasPrefix(path, "/api/media/shortvideo/admin/") || strings.HasPrefix(path, "/api/downloads/") {
			if auth.IsWANFromContext(req.Context()) || auth.UserFromContext(req.Context()).Role != auth.RoleAdmin {
				http.Error(w, "trusted environment administrator required", http.StatusForbidden)
				return
			}
		}
		if strings.HasPrefix(path, "/api/mega/") && !auth.IsTrustedLocalFromContext(req.Context()) {
			http.Error(w, "trusted local client required", http.StatusForbidden)
			return
		}
		if r.mediaReads != nil && bufferedMediaRead(path) {
			if req.Context().Err() != nil {
				http.Error(w, "media read canceled", 503)
				return
			}
			select {
			case r.mediaReads <- struct{}{}:
				defer func() { <-r.mediaReads }()
				if req.Context().Err() != nil {
					http.Error(w, "media read canceled", 503)
					return
				}
			case <-req.Context().Done():
				http.Error(w, "media read canceled", 503)
				return
			}
		}
		next.ServeHTTP(w, req)
	})
}

// Decompressed archive content and SQL image BLOBs have a per-request budget;
// admitting only two together also limits their aggregate allocation pressure.
func bufferedMediaRead(path string) bool {
	if strings.HasPrefix(path, "/api/media/text/epub-content/") {
		return true
	}
	if path == "/api/media/manga/page-list" {
		return true
	}
	switch path {
	case "/api/media/audio/metadata", "/api/media/audio/cover":
		return true
	case "/api/media/gallery/original", "/api/media/gallery/thumbnail",
		"/api/media/shortvideo/album/image", "/api/media/shortvideo/thumbnail",
		"/api/media/manga/page", "/api/media/manga/cover", "/api/media/manga/pages", "/api/media/manga/custom-cover",
		"/api/media/text/chunk", "/api/media/text/epub/book":
		return true
	}
	return false
}
