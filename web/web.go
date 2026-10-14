// Package web 提供内嵌前端静态资产。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// FS 编译嵌入的静态网页资源文件系统。
//
//go:embed dist vendor
var FS embed.FS

// Handler serves the built React UI and the offline runtimes from the same executable.
func Handler() http.Handler {
	dist, err := fs.Sub(FS, "dist")
	if err != nil {
		panic("embedded web distribution missing: " + err.Error())
	}
	app := http.FileServer(http.FS(dist))
	assets := http.FileServer(http.FS(FS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/vendor/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Referrer-Policy", "no-referrer")
			assets.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if strings.HasPrefix(r.URL.Path, "/players/") && strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-pointer-lock allow-modals allow-downloads; frame-ancestors 'self'")
			w.Header().Set("Referrer-Policy", "no-referrer")
		}
		app.ServeHTTP(w, r)
	})
}
