package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	dlassets "omnihub/features/downloads"
	"omnihub/internal/staging"
)

// 下载方案：音乐、YouTube、B 站、X 各一个标签，进去就知道下什么、怎么选、放到哪。
// 有列表的方案先「读取列表」（频道、播放列表、歌单），勾选后一次下完；全部先下到暂存区的新批次。
// 每个资源库、每个方案一份「已下载」名单（.omni_data/download-archive-<方案>.txt，yt-dlp 格式；X 另有 gallery-dl 的 sqlite；
// 按方案分开：下过视频的同一条还能用「音乐」下音频），
// 下过的——包括在暂存区删掉的——不再下，列表里也标出来。

// dlScheme 是给界面看的方案说明。
type dlScheme struct {
	Steps       []string `json:"steps"`       // 一行流程
	Destination string   `json:"destination"` // 下到哪里（移入资源库后）
	Input       string   `json:"input"`       // 输入框说明
	Placeholder string   `json:"placeholder"`
	Probe       bool     `json:"probe"` // 有「读取列表」；也可按「每个链接取」直接下载
}

func archivePath(root, scheme string) string {
	return filepath.Join(root, ".omni_data", "download-archive-"+scheme+".txt")
}

func (r *Router) schemeKinds() []*dlKind {
	libs, def := r.stagingLibraryChoice()
	library := dlField{Name: "library", Label: "暂存到", Type: "choice", Options: libs, Default: def, Help: "暂存区在资源库里；移入时同一块盘上只是改名"}
	height := dlField{Name: "max_height", Label: "最高分辨率", Type: "choice", Default: "1080", Options: []dlOption{{"2160", "4K"}, {"1440", "1440p"}, {"1080", "1080p"}, {"720", "720p"}, {"480", "480p"}, {"0", "不限"}}}
	urls := dlField{Name: "urls", Label: "链接", Type: "lines", Required: true}
	// 直接下载（不读列表）时每个链接取多少：频道、播放列表按新到旧，取前 N 个就是最新的 N 个
	perList := dlField{Name: "per_list", Label: "每个链接取", Type: "choice", Default: "30", Options: []dlOption{{"10", "最新 10 个"}, {"30", "最新 30 个"}, {"100", "最新 100 个"}, {"0", "全部"}}}
	steps := func(first string) []string {
		return []string{first, "直接下（每个链接取最新 N 个），或读取列表挑选", "下载到暂存区（下过的跳过）", "在「暂存区」试看试听、删掉不要的", "移入资源库"}
	}
	return []*dlKind{
		{
			ID: "music", Title: "音乐", Description: "一次可以填很多个歌单、专辑或单曲（YouTube、YouTube Music、B 站等），每个歌单放进自己的文件夹。只取最好的音轨，不转码，嵌入封面和标签；下过的自动跳过。",
			Scheme: &dlScheme{Steps: steps("粘贴歌单/专辑链接（一行一个）"), Destination: "音声 / 音乐 / <专辑名>/", Probe: true,
				Input: "歌单、专辑或单曲链接，一行一个", Placeholder: "https://music.youtube.com/playlist?list=…\nhttps://www.youtube.com/playlist?list=…\nhttps://www.youtube.com/watch?v=…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, perList, {Name: "album", Label: "专辑名", Type: "text", Help: "只在读取一个列表挑选时可改；直接下载时每个歌单用自己的名字"}, library},
			build:  func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("music", p) },
		},
		{
			ID: "youtube", Title: "YouTube", Description: "一次可以填很多个频道、播放列表或视频。直接下：每个取最新 N 个，排成一个任务下完，设好就不用管；也可以读取列表逐条挑。下过的自动跳过，以后同一批链接再下就只下新的。",
			Scheme: &dlScheme{Steps: steps("粘贴频道/播放列表链接（一行一个）"), Destination: "短视频 / YouTube / <频道>/", Probe: true,
				Input: "频道、播放列表或视频链接，一行一个", Placeholder: "https://www.youtube.com/@频道名\nhttps://www.youtube.com/@另一个频道\nhttps://www.youtube.com/playlist?list=…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, perList, height, library},
			build:  func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("youtube", p) },
		},
		{
			ID: "bilibili", Title: "B 站", Description: "一次可以填很多个 UP 主、合集或视频。直接下：每个取最新 N 个，设好就不用管；也可以读取列表逐条挑。下过的自动跳过。",
			Scheme: &dlScheme{Steps: steps("粘贴 UP 主空间/合集链接（一行一个）"), Destination: "短视频 / B站 / <UP主>/", Probe: true,
				Input: "UP 主空间、合集或视频链接，一行一个", Placeholder: "https://space.bilibili.com/<UID>/video\nhttps://www.bilibili.com/video/BV…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, perList, height, library},
			build:  func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("bilibili", p) },
		},
		{
			ID: "x", Title: "X", Description: "下载用户发过的图片和视频（gallery-dl，需要在「账号与 Cookie」里读过 X 的登录）。图片移入时并进相册，视频进短视频。",
			Scheme: &dlScheme{Steps: []string{"填用户名（一行一个）", "选图片/视频、最近多少个", "下载到暂存区", "在「暂存区」看图、删掉不要的", "移入资源库"},
				Destination: "图片 → 相册；视频 → 短视频 / X / <用户>/", Input: "用户名或主页链接，一行一个", Placeholder: "elonmusk\n@nasa\nhttps://x.com/<用户>/media"},
			needs: []string{"uv"},
			Fields: []dlField{urls,
				{Name: "x_media", Label: "要什么", Type: "choice", Default: "all", Options: []dlOption{{"all", "图片和视频"}, {"image", "只要图片"}, {"video", "只要视频"}}},
				{Name: "x_limit", Label: "每个用户最多", Type: "choice", Default: "200", Options: []dlOption{{"50", "最近 50 个"}, {"200", "最近 200 个"}, {"1000", "最近 1000 个"}, {"0", "全部"}}},
				library},
			build: func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("x", p) },
		},
		{
			ID: "other", Title: "其他链接", Description: "以上方案之外的网站（抖音、TikTok 等 yt-dlp 支持的站点）。按域名认平台，视频进短视频。",
			Scheme: &dlScheme{Steps: []string{"粘贴链接（一行一个）", "下载到暂存区", "在「暂存区」试看、删掉不要的", "移入资源库"},
				Destination: "短视频 / <平台或网站>/ <作者>/", Input: "视频链接，一行一个", Placeholder: "https://www.douyin.com/video/…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, height, library},
			build:  func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("other", p) },
		},
	}
}

// buildScheme 建暂存批次并组好下载脚本的参数。
func (r *Router) buildScheme(scheme string, p dlParams) (dlPlan, error) {
	urls := p.lines("urls")
	if len(urls) == 0 || len(urls) > 2000 {
		return dlPlan{}, errors.New("请给出 1–2000 个链接")
	}
	for _, u := range urls {
		if scheme != "x" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return dlPlan{}, fmt.Errorf("不是网址：%s", u)
		}
	}
	lib, err := r.writableLibrary(p.text("library"))
	if err != nil {
		return dlPlan{}, err
	}
	titles := map[string]string{"music": "音乐", "youtube": "YouTube", "bilibili": "B站", "x": "X", "other": "下载"}
	label := titles[scheme]
	if a := p.text("album"); a != "" {
		label += " " + a
	} else if t := p.text("title"); t != "" {
		label += " " + t
	} else if scheme == "x" {
		label += " " + strings.Join(urls[:min(3, len(urls))], " ")
	} else {
		label += fmt.Sprintf(" %d 个链接", len(urls))
	}
	batch, err := staging.NewBatch(lib.RootPath, label, scheme)
	if err != nil {
		return dlPlan{}, err
	}
	dir, _ := staging.BatchDir(lib.RootPath, batch)
	if err := os.MkdirAll(filepath.Dir(archivePath(lib.RootPath, scheme)), 0o755); err != nil {
		return dlPlan{}, err
	}
	args := append([]string{"scripts/general.py"}, urls...)
	args = append(args, "--batch", dir, "--scheme", scheme, "--archive", archivePath(lib.RootPath, scheme))
	if h := p.text("max_height"); h != "" {
		args = append(args, "--max-height", h)
	}
	if a := p.text("album"); a != "" {
		args = append(args, "--album", a)
	}
	if n := p.text("per_list"); n != "" && p.text("mode") == "direct" {
		args = append(args, "--per-list", n)
	}
	if m := p.text("x_media"); m != "" {
		args = append(args, "--x-media", m)
	}
	if n := p.text("x_limit"); n != "" {
		args = append(args, "--x-limit", n)
	}
	cleanup := func(context.Context) error { // 什么也没下到时删掉空批次
		if _, n, err := staging.Files(lib.RootPath, batch, "", 0, 1); err == nil && n == 0 {
			return os.RemoveAll(dir)
		}
		return nil
	}
	return dlPlan{title: label, args: args, resource: dir, lib: lib, after: cleanup}, nil
}

// probeEntry 是列表里的一条（probe.py 给出，服务端补上是否下载过）。
type probeEntry struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Duration   *float64 `json:"duration"`
	Views      *int64   `json:"views"`
	Likes      *int64   `json:"likes"`
	Uploader   string   `json:"uploader"`
	Date       string   `json:"date"`
	Downloaded bool     `json:"downloaded"`
}

// probeDownload 读取频道/播放列表/歌单的条目（只读元数据），并按资源库的已下载名单标出下过的。
func (r *Router) probeDownload(w http.ResponseWriter, req *http.Request, body []byte) {
	var in struct {
		URL     string `json:"url"`
		Library string `json:"library"`
		Scheme  string `json:"scheme"`
	}
	if json.Unmarshal(body, &in) != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		http.Error(w, "请粘贴一个网址", http.StatusBadRequest)
		return
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		http.Error(w, "本机没有 uv", http.StatusServiceUnavailable)
		return
	}
	project, err := dlassets.Prepare(filepath.Join(r.cacheDir, "downloads"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	env := append(os.Environ(), "UV_PROJECT_ENVIRONMENT="+filepath.Join(project, "venv"), "PYTHONUNBUFFERED=1")
	if p := platformOf(u.Hostname()); p != nil {
		if f := r.CookieFile(p.ID); f != "" {
			env = append(env, "OMNIHUB_COOKIES_FILE="+f)
		}
	}
	r.downloads.mu.Lock()
	if proxy := r.downloads.settings.YtdlpProxy; proxy != "" {
		env = append(env, "OMNIHUB_YTDLP_PROXY="+proxy)
	}
	r.downloads.mu.Unlock()
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, uv, "run", "--frozen", "--project", project, "python", filepath.Join(project, "scripts", "probe.py"), u.String())
	cmd.Dir, cmd.Env = project, env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	var result struct {
		Title    string       `json:"title"`
		Uploader string       `json:"uploader"`
		URL      string       `json:"url"`
		Entries  []probeEntry `json:"entries"`
	}
	found := false
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "OMNIHUB_PROBE "); ok && json.Unmarshal([]byte(rest), &result) == nil {
			found = true
		}
	}
	if !found {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		http.Error(w, "读取列表失败："+msg, http.StatusBadGateway)
		return
	}
	if lib, err := r.writableLibrary(in.Library); err == nil {
		if data, err := os.ReadFile(archivePath(lib.RootPath, filepath.Base(in.Scheme))); in.Scheme != "" && err == nil {
			done := map[string]bool{}
			for _, line := range strings.Split(string(data), "\n") {
				if _, id, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
					done[id] = true
				}
			}
			for i := range result.Entries {
				result.Entries[i].Downloaded = done[result.Entries[i].ID]
			}
		}
	}
	writeJSON(w, http.StatusOK, result)
}
