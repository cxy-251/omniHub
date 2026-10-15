package server

import (
	"encoding/base64"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/archiveutil"
	"omnihub/internal/text"
)

const epubContentPrefix = "/api/media/text/epub-content/"

func epubAssetBase(logical string) string {
	return epubContentPrefix + base64.RawURLEncoding.EncodeToString([]byte(logical)) + "/"
}

func (r *Router) handleTextEpub(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.Error(w, "method not allowed", 405)
		return
	}
	logical, _, ok := r.canonicalTextFor(w, req, req.URL.Query().Get("path"))
	if !ok {
		return
	}
	entry := req.URL.Query().Get("entry")
	if len(entry) > 4096 || !filepath.IsLocal(filepath.FromSlash(entry)) || strings.ContainsAny(entry, "\\\x00:") {
		http.Error(w, "invalid EPUB entry", 400)
		return
	}
	location := (&url.URL{Path: epubAssetBase(logical) + entry}).String()
	http.Redirect(w, req, location, http.StatusTemporaryRedirect)
}

// canonicalTextFor 规范化文档 ID 并校验子分类等级；失败时已写好响应。
func (r *Router) canonicalTextFor(w http.ResponseWriter, req *http.Request, id string) (string, string, bool) {
	logical, physical, err := r.canonicalText(id)
	if err != nil {
		http.Error(w, "document unavailable", 404)
		return "", "", false
	}
	if !r.allowPath(w, req, logical) {
		return "", "", false
	}
	return logical, physical, true
}

func (r *Router) canonicalText(id string) (string, string, error) {
	if id == "" || len(id) > 4096 {
		return "", "", errors.New("invalid document identity")
	}
	selected := r.libMgr.GetPrimary()
	if prefix, _, ok := strings.Cut(id, ":"); ok {
		selected = nil
		for _, lib := range r.libMgr.ListLibraries() {
			if lib.ID == prefix {
				selected = lib
				break
			}
		}
	}
	if selected == nil || !selected.IsOnline {
		return "", "", errors.New("document library unavailable")
	}
	physical, err := r.libMgr.ResolveCategoryPath(id, "media_library/docs", "media_library/novels")
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(selected.RootPath, physical)
	if err != nil {
		return "", "", err
	}
	return selected.ID + ":" + filepath.ToSlash(relative), physical, nil
}

func (r *Router) handleEpubBook(w http.ResponseWriter, req *http.Request) {
	logical, physical, ok := r.canonicalTextFor(w, req, req.URL.Query().Get("path"))
	if !ok {
		return
	}
	book, err := text.InspectEpub(req.Context(), physical)
	if err != nil {
		http.Error(w, "invalid or unsupported EPUB", 422)
		return
	}
	writeJSON(w, 200, struct {
		*text.EpubBook
		LogicalPath string `json:"logical_path"`
		AssetBase   string `json:"asset_base"`
	}{book, logical, epubAssetBase(logical)})
}

func (r *Router) handleEpubContent(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.Error(w, "method not allowed", 405)
		return
	}
	token, entry, ok := strings.Cut(strings.TrimPrefix(req.URL.Path, epubContentPrefix), "/")
	if !ok || len(token) > 5500 || len(entry) > 4096 || !filepath.IsLocal(filepath.FromSlash(entry)) || strings.ContainsAny(entry, "\\\x00:") {
		http.Error(w, "invalid EPUB asset", 400)
		return
	}
	logical, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		http.Error(w, "invalid document identity", 400)
		return
	}
	_, physical, ok := r.canonicalTextFor(w, req, string(logical))
	if !ok {
		return
	}
	ext := strings.ToLower(filepath.Ext(entry))
	contentType := mime.TypeByExtension(ext)
	switch ext {
	case ".xhtml", ".html", ".htm":
		contentType = "text/html; charset=utf-8"
	case ".css", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".avif", ".woff", ".woff2", ".ttf", ".otf":
	default:
		http.Error(w, "unsupported EPUB asset type", 415)
		return
	}
	data, err := text.ReadEpubEntryContext(req.Context(), physical, entry)
	if err != nil {
		if errors.Is(err, archiveutil.ErrCapacity) {
			http.Error(w, "EPUB asset exceeds read capacity", 413)
		} else {
			http.Error(w, "EPUB asset unavailable", 404)
		}
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Same-origin permits authenticated local images/styles; scripts remain blocked
	// by both the response sandbox and the reader iframe's sandbox attribute.
	w.Header().Set("Content-Security-Policy", "sandbox allow-same-origin; default-src 'none'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Type", contentType)
	if info, err := os.Stat(physical); err == nil {
		w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	w.WriteHeader(200)
	if req.Method == http.MethodGet {
		if _, err := w.Write(data); err != nil {
			return
		}
	}
}
