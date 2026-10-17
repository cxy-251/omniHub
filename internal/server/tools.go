package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"omnihub/internal/auth"
	"omnihub/internal/tools"
)

// 局域网小工具：剪贴板、共享文件、硬件看板、测速、唤醒与局域网扫描。
// 都按「工具」分区等级放行；会动到本机或局域网的操作（上传、唤醒、扫描）另外不许从外网来。
const (
	maxClipboardBytes = 64 << 10
	maxUploadChunk    = 8 << 20
)

// 每个工具单独分级（子分区 tools/<名字>，在设置页修改）：剪贴板、共享文件默认「用户」，
// 主机状态、网络默认「管理员」。「工具」分区本身不再设门槛：能用至少一个工具就显示。
var toolDefaults = []struct {
	name  string
	level int
}{{"clipboard", auth.LevelVIP0}, {"shared", auth.LevelVIP0}, {"status", auth.LevelAdmin}, {"network", auth.LevelAdmin}}

// allowedTools 返回当前账号能用的工具名。
func (r *Router) allowedTools(req *http.Request) ([]string, error) {
	stored, err := r.authDB.ListSectionPermissions()
	if err != nil {
		return nil, err
	}
	level := auth.UserFromContext(req.Context()).Level
	var out []string
	for _, t := range toolDefaults {
		required := t.level
		if v, ok := stored["tools/"+t.name]; ok {
			required = v
		}
		if level >= required {
			out = append(out, t.name)
		}
	}
	return out, nil
}

func (r *Router) toolsAllowed(w http.ResponseWriter, req *http.Request, tool string, lanOnly bool) bool {
	if !r.sectionEnabled("tools") {
		http.Error(w, "tools disabled", http.StatusForbidden)
		return false
	}
	allowed, err := r.allowedTools(req)
	if err != nil {
		http.Error(w, "permission lookup failed", http.StatusServiceUnavailable)
		return false
	}
	ok := false
	for _, name := range allowed {
		ok = ok || name == tool
	}
	if !ok {
		http.Error(w, "forbidden: insufficient permission level", http.StatusForbidden)
		return false
	}
	if lanOnly && auth.IsWANFromContext(req.Context()) {
		http.Error(w, "forbidden: LAN only", http.StatusForbidden)
		return false
	}
	return true
}

func (r *Router) sharedDir() (string, error) {
	primary := r.libMgr.GetPrimary()
	if primary == nil || !primary.IsOnline {
		return "", errors.New("no primary library")
	}
	if primary.ReadOnly {
		return "", errors.New("primary library is read-only")
	}
	return r.libMgr.ResolveCategoryPath(primary.ID+":media_library/shared", "media_library/shared")
}

func (r *Router) handleToolsClipboard(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "clipboard", false) {
		return
	}
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, r.clipboard.List())
	case http.MethodPost:
		var p struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxClipboardBytes+1024)).Decode(&p); err != nil || strings.TrimSpace(p.Content) == "" || len(p.Content) > maxClipboardBytes {
			http.Error(w, "invalid content (max 64KB)", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, r.clipboard.Push(p.Content))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleToolsMetrics(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "status", false) {
		return
	}
	// 磁盘看资源库所在的盘（系统根分区在 SteamOS 上只读、很小，没有参考意义）。
	disk := "/"
	if primary := r.libMgr.GetPrimary(); primary != nil && primary.IsOnline {
		disk = primary.RootPath
	}
	writeJSON(w, http.StatusOK, tools.GetSystemMetrics(disk))
}

func (r *Router) handleToolsWOL(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !r.toolsAllowed(w, req, "network", true) {
		return
	}
	var p struct {
		MAC string `json:"mac"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1024)).Decode(&p); err != nil || p.MAC == "" {
		http.Error(w, "invalid mac address", http.StatusBadRequest)
		return
	}
	if err := tools.SendWakeOnLAN(p.MAC); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleToolsLANScan(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "network", true) {
		return
	}
	devices, err := tools.ScanLAN()
	if err != nil || devices == nil {
		devices = []tools.LANDevice{}
	}
	writeJSON(w, http.StatusOK, devices)
}

func (r *Router) handleToolsSharedFiles(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "shared", false) {
		return
	}
	dir, err := r.sharedDir()
	if err != nil {
		writeJSON(w, http.StatusOK, []tools.SharedFile{})
		return
	}
	list, err := tools.ListSharedFiles(dir)
	if err != nil {
		http.Error(w, "shared directory unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleToolsSharedUpload 分块上传：chunk_index 从 0 开始，每块最多 8MB，最后一块带 last=1。
// 写到隐藏的 .upload-<名字>.part，最后一块收完才改成正式文件名；同名文件已存在时拒绝（不覆盖）。
func (r *Router) handleToolsSharedUpload(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !r.toolsAllowed(w, req, "shared", true) {
		return
	}
	dir, err := r.sharedDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	q := req.URL.Query()
	chunk, err := strconv.Atoi(q.Get("chunk_index"))
	if err != nil || chunk < 0 {
		http.Error(w, "invalid chunk_index", http.StatusBadRequest)
		return
	}
	err = tools.WriteChunk(dir, q.Get("filename"), chunk, q.Get("last") == "1", http.MaxBytesReader(w, req.Body, maxUploadChunk))
	switch {
	case errors.Is(err, tools.ErrInvalidName):
		http.Error(w, "invalid filename", http.StatusBadRequest)
	case errors.Is(err, fs.ErrExist):
		http.Error(w, "同名文件已存在", http.StatusConflict)
	case err != nil:
		http.Error(w, "upload failed: "+err.Error(), http.StatusInternalServerError)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleToolsSharedFile 下载一个共享文件。一律作为附件下载、禁止嗅探：上传的 .html 不能在本站源里执行。
func (r *Router) handleToolsSharedFile(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "shared", false) {
		return
	}
	dir, err := r.sharedDir()
	if err != nil {
		http.NotFound(w, req)
		return
	}
	name := req.URL.Query().Get("name")
	file, info, err := tools.OpenShared(dir, name)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
	http.ServeContent(w, req, "", info.ModTime(), file)
}

// handleToolsSharedZip 把选中的文件（files=a,b；为空表示全部）打包下载。
func (r *Router) handleToolsSharedZip(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "shared", false) {
		return
	}
	dir, err := r.sharedDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	var names []string
	if files := req.URL.Query().Get("files"); files != "" {
		names = strings.Split(files, ",")
	} else {
		list, err := tools.ListSharedFiles(dir)
		if err != nil {
			http.Error(w, "shared directory unavailable", http.StatusServiceUnavailable)
			return
		}
		for _, f := range list {
			if !f.IsDir {
				names = append(names, f.Name)
			}
		}
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="shared.zip"`)
	_ = tools.StreamZipArchive(dir, names, w)
}

func (r *Router) handleToolsSpeed(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "network", false) {
		return
	}
	switch req.Method {
	case http.MethodGet: // 下行：5MB 零字节
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(5<<20))
		_, _ = w.Write(make([]byte, 5<<20))
	case http.MethodPost: // 上行：读完丢弃（最多 32MB）
		n, _ := io.Copy(io.Discard, http.MaxBytesReader(w, req.Body, 32<<20))
		writeJSON(w, http.StatusOK, map[string]int64{"bytes": n})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleToolsWebShell 暂时停用：旧实现没有超时、输出上限和进程组清理。
func (r *Router) handleToolsWebShell(w http.ResponseWriter, req *http.Request) {
	http.Error(w, "web shell is disabled", http.StatusServiceUnavailable)
}
