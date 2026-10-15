package application

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"omnihub/internal/library"
)

type browseEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsLibrary bool   `json:"is_library"`
}

// browseDirectory 列出服务端目录的子目录，供管理员在界面上点选资源库根目录。
// 空路径返回常用起点：用户主目录、可移动存储挂载点与根目录。只列目录，不返回文件。
func browseDirectory(w http.ResponseWriter, path string) {
	if path == "" {
		respond(w, map[string]any{"path": "", "parent": "", "entries": browseRoots()})
		return
	}
	if !filepath.IsAbs(path) || len(path) > 4096 {
		http.Error(w, "absolute path required", http.StatusBadRequest)
		return
	}
	path = filepath.Clean(path)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "directory unavailable", http.StatusNotFound)
		return
	}
	defer f.Close()
	entries, err := f.ReadDir(5001)
	if err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "directory unreadable", http.StatusForbidden)
		return
	}
	list := []browseEntry{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(path, e.Name())
		if info, err := os.Stat(full); err != nil || !info.IsDir() {
			continue
		}
		list = append(list, browseEntry{Name: e.Name(), Path: full, IsLibrary: hasMarker(full)})
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	respond(w, map[string]any{"path": path, "parent": parent, "is_library": hasMarker(path), "truncated": len(entries) > 5000, "entries": list})
}

func browseRoots() []browseEntry {
	var roots []browseEntry
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, browseEntry{Name: "主目录", Path: home})
	}
	for _, m := range removableMounts() {
		roots = append(roots, browseEntry{Name: "移动存储 " + filepath.Base(m), Path: m, IsLibrary: hasMarker(m)})
	}
	return append(roots, browseEntry{Name: "根目录 /", Path: string(filepath.Separator)})
}

func hasMarker(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, library.LibraryMarkerFile))
	return err == nil
}

// removableMounts 读取 /proc/mounts 中位于 /run/media、/media、/mnt 下的挂载点（SD 卡、U 盘）。
// 不用目录通配：SteamOS 会在 /run/media 下额外放指向挂载点的软链接。
func removableMounts() []string {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}
	var mounts []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// /proc/mounts 把空格等字符写成八进制转义。
		target := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(fields[1])
		for _, prefix := range []string{"/run/media/", "/media/", "/mnt/"} {
			if strings.HasPrefix(target, prefix) {
				mounts = append(mounts, target)
				break
			}
		}
	}
	sort.Strings(mounts)
	return mounts
}
