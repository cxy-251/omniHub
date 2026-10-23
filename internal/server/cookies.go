package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	dlassets "omnihub/features/downloads"
	"omnihub/internal/auth"
)

// 账号与 Cookie：下载工具（yt-dlp、gallery-dl）要用各平台的登录状态。用户在自己的 Chrome 里正常登录，
// 这里按平台从 Chrome 读出该平台域名下的 Cookie，各存一份 cookies.txt（配置目录 cookies/，仅本人可读）。
// 只在用户点了才读，只读该平台的域名；接口与页面都不出现 Cookie 的值。只给本机桌面端用。
//   GET  /api/cookies                     Chrome 目录与各平台状态
//   POST /api/cookies/read  {platform}    从 Chrome 读取
//   POST /api/cookies/clear {platform}    删除已存的
//   POST /api/cookies/profile {path}      指定 Chrome 用户数据目录（空 = 自动找）

type cookiePlatform struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Domains []string `json:"domains"`
	login   []string // 表示已登录的 Cookie 名（推测，各平台网页版常见的会话 Cookie）
}

var cookiePlatforms = []cookiePlatform{
	{ID: "youtube", Label: "YouTube", Domains: []string{"youtube.com", "google.com"}, login: []string{"SAPISID", "LOGIN_INFO", "__Secure-3PSID"}},
	{ID: "bilibili", Label: "B 站", Domains: []string{"bilibili.com"}, login: []string{"SESSDATA"}},
	{ID: "x", Label: "X（推特）", Domains: []string{"x.com", "twitter.com"}, login: []string{"auth_token"}},
	{ID: "douyin", Label: "抖音", Domains: []string{"douyin.com"}, login: []string{"sessionid", "sessionid_ss"}},
	{ID: "kuaishou", Label: "快手", Domains: []string{"kuaishou.com"}, login: []string{"kuaishou.server.web_st", "kuaishou.server.webday7_st", "userId"}},
	{ID: "tiktok", Label: "TikTok", Domains: []string{"tiktok.com"}, login: []string{"sessionid"}},
	{ID: "xiaohongshu", Label: "小红书", Domains: []string{"xiaohongshu.com"}, login: []string{"web_session"}},
}

// cookieInfo 是某个平台已存 Cookie 的概要（存在 cookies/<平台>.json）。
type cookieInfo struct {
	Count    int       `json:"count"`
	LoggedIn bool      `json:"logged_in"`
	Expires  float64   `json:"expires,omitempty"` // 登录 Cookie 最早的过期时间（Unix 秒）
	SavedAt  time.Time `json:"saved_at"`
}

func (r *Router) cookieDir() string {
	r.downloads.mu.Lock()
	defer r.downloads.mu.Unlock()
	return filepath.Join(r.downloads.configDir, "cookies")
}

// CookieFile 返回某平台已存的 cookies.txt；没有时返回空串（下载任务按平台取用）。
func (r *Router) CookieFile(platform string) string {
	p := filepath.Join(r.cookieDir(), platform+".txt")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// chromeProfile 返回 Chrome 用户数据目录：设置里指定的，否则按常见位置找第一个有 Cookie 库的。
func (r *Router) chromeProfile() (string, []string) {
	r.downloads.mu.Lock()
	configured := r.downloads.settings.ChromeProfile
	r.downloads.mu.Unlock()
	home, _ := os.UserHomeDir()
	var found []string
	for _, dir := range []string{
		filepath.Join(home, "Applications", "chrome", "data", "profile"), // 便携版 Chrome（~/Applications/chrome/launch.sh）
		filepath.Join(home, ".config", "google-chrome"),
		filepath.Join(home, ".config", "chromium"),
		filepath.Join(home, ".var", "app", "com.google.Chrome", "config", "google-chrome"),
	} {
		for _, sub := range []string{"Default/Cookies", "Default/Network/Cookies"} {
			if _, err := os.Stat(filepath.Join(dir, sub)); err == nil {
				found = append(found, dir)
				break
			}
		}
	}
	if configured != "" {
		return configured, found
	}
	if len(found) > 0 {
		return found[0], found
	}
	return "", found
}

func (r *Router) handleCookies(w http.ResponseWriter, req *http.Request) {
	if !auth.IsTrustedLocalFromContext(req.Context()) {
		http.Error(w, "只能在 Deck 本机的桌面端管理 Cookie", http.StatusForbidden)
		return
	}
	action := strings.TrimPrefix(strings.TrimPrefix(req.URL.Path, "/api/cookies"), "/")
	if req.Method == http.MethodGet && action == "" {
		r.cookieStatus(w)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Platform string `json:"platform"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096)).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if action == "profile" {
		if in.Path != "" && !filepath.IsAbs(in.Path) {
			http.Error(w, "路径要是绝对路径", http.StatusBadRequest)
			return
		}
		r.downloads.mu.Lock()
		next := r.downloads.settings
		next.ChromeProfile = in.Path
		var err error
		if r.downloads.save != nil {
			err = r.downloads.save(next)
		}
		if err == nil {
			r.downloads.settings = next
		}
		r.downloads.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		r.cookieStatus(w)
		return
	}
	var platform *cookiePlatform
	for i := range cookiePlatforms {
		if cookiePlatforms[i].ID == in.Platform {
			platform = &cookiePlatforms[i]
		}
	}
	if platform == nil {
		http.Error(w, "unknown platform", http.StatusBadRequest)
		return
	}
	dir := r.cookieDir()
	switch action {
	case "clear":
		for _, ext := range []string{".txt", ".json"} {
			if err := os.Remove(filepath.Join(dir, platform.ID+ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	case "read":
		if err := r.readChromeCookies(req.Context(), platform, dir); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	default:
		http.NotFound(w, req)
		return
	}
	r.cookieStatus(w)
}

func (r *Router) cookieStatus(w http.ResponseWriter) {
	profile, found := r.chromeProfile()
	type row struct {
		cookiePlatform
		Saved *cookieInfo `json:"saved,omitempty"`
	}
	rows := make([]row, 0, len(cookiePlatforms))
	dir := r.cookieDir()
	for _, p := range cookiePlatforms {
		entry := row{cookiePlatform: p}
		if data, err := os.ReadFile(filepath.Join(dir, p.ID+".json")); err == nil {
			var info cookieInfo
			if json.Unmarshal(data, &info) == nil {
				entry.Saved = &info
			}
		}
		rows = append(rows, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile, "profiles_found": found, "platforms": rows})
}

// readChromeCookies 用下载环境里的 yt-dlp 从 Chrome 读出该平台的 Cookie（第一次要建虚拟环境，可能要等一会儿）。
func (r *Router) readChromeCookies(ctx context.Context, p *cookiePlatform, dir string) error {
	profile, _ := r.chromeProfile()
	if profile == "" {
		return errors.New("没有找到 Chrome 的用户数据目录，请在页面上指定")
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		return errors.New("本机没有 uv")
	}
	project, err := dlassets.Prepare(filepath.Join(r.cacheDir, "downloads"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	args := []string{"run", "--frozen", "--project", project, "python", filepath.Join(project, "scripts", "cookies.py"),
		"--profile", profile, "--login", strings.Join(p.login, ","), "--out", filepath.Join(dir, p.ID+".txt")}
	for _, d := range p.Domains {
		args = append(args, "--domain", d)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, uv, args...)
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "UV_PROJECT_ENVIRONMENT="+filepath.Join(project, "venv"), "PYTHONUNBUFFERED=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var info cookieInfo
	found := false
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if rest, ok := strings.CutPrefix(scanner.Text(), "OMNIHUB_RESULT "); ok && json.Unmarshal([]byte(rest), &info) == nil {
			found = true
		}
	}
	if err != nil || !found {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return errors.New("读取失败：" + msg)
	}
	info.SavedAt = time.Now()
	data, _ := json.Marshal(info)
	return os.WriteFile(filepath.Join(dir, p.ID+".json"), data, 0o600)
}
