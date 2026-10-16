package server

import (
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"omnihub/internal/auth"
	"omnihub/internal/ordering"
)

// 文档（media_library/docs 下的 md/rst）：网页端按目录浏览、整篇渲染（公式、图表需要看到完整文件）。
const docsCategory = "media_library/docs"
const maxDocBytes = 4 << 20

type docEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"` // 逻辑路径「库ID:media_library/docs/…」
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size,omitempty"`
	Library string `json:"library,omitempty"`
}

func isDocFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".rst", ".txt":
		return true
	}
	return false
}

// handleDocsTree 列出一个文档目录：dir 为空时列出各在线库的 docs 根目录内容（合并）；
// 否则 dir 是逻辑路径。只返回子目录与 md/rst/txt，按子分区等级过滤，隐藏文件跳过。
func (r *Router) handleDocsTree(w http.ResponseWriter, req *http.Request) {
	if !r.checkSectionAccess(w, req, "text", auth.TargetAll) {
		return
	}
	access, err := r.accessFor(req)
	if err != nil {
		http.Error(w, "permission store unavailable", 503)
		return
	}
	dir := req.URL.Query().Get("dir")
	var dirs []string
	if dir == "" {
		for _, lib := range r.libMgr.ListLibraries() {
			if lib.IsOnline {
				dirs = append(dirs, lib.ID+":"+docsCategory)
			}
		}
	} else {
		if !access.allows(dir) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		dirs = []string{dir}
	}
	out := []docEntry{}
	for _, logical := range dirs {
		physical, err := r.libMgr.ResolveCategoryPath(logical, docsCategory)
		if err != nil {
			if dir != "" {
				http.Error(w, "directory not found", 404)
				return
			}
			continue
		}
		entries, err := os.ReadDir(physical)
		if err != nil {
			if dir != "" {
				http.Error(w, "directory unavailable", 404)
				return
			}
			continue
		}
		if len(entries) > 20000 {
			entries = entries[:20000]
		}
		lib, _, _ := strings.Cut(logical, ":")
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || e.Type()&os.ModeSymlink != 0 {
				continue
			}
			child := strings.TrimSuffix(logical, "/") + "/" + name
			if !access.allows(child) {
				continue
			}
			if e.IsDir() {
				out = append(out, docEntry{Name: name, Path: child, IsDir: true, Library: lib})
			} else if isDocFile(name) {
				info, err := e.Info()
				if err == nil {
					out = append(out, docEntry{Name: name, Path: child, Size: info.Size(), Library: lib})
				}
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].IsDir != out[b].IsDir {
			return out[a].IsDir
		}
		return ordering.NaturalLess(out[a].Name, out[b].Name)
	})
	writeJSON(w, http.StatusOK, out)
}

// handleDocRaw 返回整篇文档（UTF-8 文本，最多 4MB）。
func (r *Router) handleDocRaw(w http.ResponseWriter, req *http.Request) {
	logical := req.URL.Query().Get("path")
	if !r.allowPath(w, req, logical) {
		return
	}
	if !isDocFile(logical) || strings.Contains(path.Clean("/"+logical), "..") {
		http.Error(w, "unsupported document", http.StatusBadRequest)
		return
	}
	physical, err := r.libMgr.ResolveCategoryPath(logical, docsCategory, "media_library/novels")
	if err != nil {
		http.Error(w, "document not found", 404)
		return
	}
	f, err := os.Open(physical)
	if err != nil {
		http.Error(w, "document not found", 404)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "document unavailable", 404)
		return
	}
	if info.Size() > maxDocBytes {
		http.Error(w, "文档超过 4MB，请在桌面端或用分段阅读打开", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(f, maxDocBytes))
}

