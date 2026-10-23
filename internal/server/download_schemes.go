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
	// 直接下载（不读列表）时怎么挑：先提取每个链接的单个视频、逐个补充数据（音乐再查 Last.fm），按分数取前 N 个
	rankField := func(options []dlOption, def string) dlField {
		return dlField{Name: "rank", Label: "按什么挑", Type: "choice", Default: def, Options: options}
	}
	musicRank := rankField([]dlOption{{"replay", "重播率（Last.fm）"}, {"ratio", "点赞率"}, {"views", "播放量"}, {"none", "不挑，全下"}}, "replay")
	videoRank := rankField([]dlOption{{"ratio", "点赞率"}, {"views", "播放量"}, {"none", "不挑，全下"}}, "ratio")
	top := dlField{Name: "top", Label: "每个链接取", Type: "choice", Default: "30", Options: []dlOption{{"10", "前 10 个"}, {"30", "前 30 个"}, {"100", "前 100 个"}}}
	pool := dlField{Name: "pool", Label: "在前多少个里排名", Type: "choice", Default: "200", Options: []dlOption{{"100", "前 100 个"}, {"200", "前 200 个"}, {"500", "前 500 个"}, {"0", "全部（很慢）"}},
		Help: "排名要逐个读每个视频的数据，每个约 1～2 秒"}
	steps := func(first string) []string {
		return []string{first, "提取每个链接里的单个视频", "逐个读数据、按质量排名取前 N 个（或读取列表自己挑）", "依次下载到暂存区（下过的跳过）", "在「暂存区」试看试听、删掉不要的", "移入资源库"}
	}
	return []*dlKind{
		{
			ID: "music", Title: "音乐", Description: "一次可以填很多个歌单、专辑或单曲（YouTube、YouTube Music、B 站等），直接下时按重播率（Last.fm：总播放 ÷ 听众，听过的人平均听几遍）或点赞率取前 N 首，每个歌单放进自己的文件夹。只取最好的音轨，不转码，嵌入封面和标签；下过的自动跳过。",
			Scheme: &dlScheme{Steps: steps("粘贴歌单/专辑链接（一行一个）"), Destination: "音声 / 音乐 / <专辑名>/", Probe: true,
				Input: "歌单、专辑或单曲链接，一行一个", Placeholder: "https://music.youtube.com/playlist?list=…\nhttps://www.youtube.com/playlist?list=…\nhttps://www.youtube.com/watch?v=…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, musicRank, top, pool, {Name: "album", Label: "专辑名", Type: "text", Help: "只在读取一个列表挑选时可改；直接下载时每个歌单用自己的名字"}, library},
			build:  func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("music", p) },
		},
		{
			ID: "youtube", Title: "YouTube", Description: "一次可以填很多个频道、播放列表或视频。直接下：逐个读每个视频的数据，按点赞率或播放量取前 N 个，排成一个任务下完，设好就不用管；也可以读取列表逐条挑。下过的自动跳过。",
			Scheme: &dlScheme{Steps: steps("粘贴频道/播放列表链接（一行一个）"), Destination: "短视频 / YouTube / <频道>/", Probe: true,
				Input: "频道、播放列表或视频链接，一行一个", Placeholder: "https://www.youtube.com/@频道名\nhttps://www.youtube.com/@另一个频道\nhttps://www.youtube.com/playlist?list=…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, videoRank, top, pool, height, library},
			build:  func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("youtube", p) },
		},
		{
			ID: "bilibili", Title: "B 站", Description: "一次可以填很多个 UP 主、合集或视频。直接下：逐个读数据，按点赞率或播放量取前 N 个，设好就不用管；也可以读取列表逐条挑。下过的自动跳过。",
			Scheme: &dlScheme{Steps: steps("粘贴 UP 主空间/合集链接（一行一个）"), Destination: "短视频 / B站 / <UP主>/", Probe: true,
				Input: "UP 主空间、合集或视频链接，一行一个", Placeholder: "https://space.bilibili.com/<UID>/video\nhttps://www.bilibili.com/video/BV…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, videoRank, top, pool, height, library},
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
	if p.text("mode") == "direct" {
		if rank := p.text("rank"); rank != "" {
			args = append(args, "--rank", rank, "--top", p.text("top"), "--pool", p.text("pool"))
		}
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
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Minute)
	defer cancel()
	out, stderrText, runErr := r.runDownloadScript(ctx, u.Hostname(), "probe.py", u.String())
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
		msg := strings.TrimSpace(stderrText)
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

// runDownloadScript 在下载环境里运行 features/downloads/scripts 下的一个脚本（同步），带上 host 所属平台的
// 登录 Cookie、代理与 Last.fm key；返回标准输出与标准错误。
func (r *Router) runDownloadScript(ctx context.Context, host, script string, args ...string) ([]byte, string, error) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		return nil, "", errors.New("本机没有 uv")
	}
	project, err := dlassets.Prepare(filepath.Join(r.cacheDir, "downloads"))
	if err != nil {
		return nil, "", err
	}
	env := append(os.Environ(), "UV_PROJECT_ENVIRONMENT="+filepath.Join(project, "venv"), "PYTHONUNBUFFERED=1")
	if p := platformOf(host); p != nil {
		if f := r.CookieFile(p.ID); f != "" {
			env = append(env, "OMNIHUB_COOKIES_FILE="+f)
		}
	}
	r.downloads.mu.Lock()
	if proxy := r.downloads.settings.YtdlpProxy; proxy != "" {
		env = append(env, "OMNIHUB_YTDLP_PROXY="+proxy)
	}
	if key := r.downloads.settings.LastfmKey; key != "" {
		env = append(env, "OMNIHUB_LASTFM_KEY="+key)
	}
	r.downloads.mu.Unlock()
	cmd := exec.CommandContext(ctx, uv, append([]string{"run", "--frozen", "--project", project, "python", filepath.Join(project, "scripts", script)}, args...)...)
	cmd.Dir, cmd.Env = project, env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return out, stderr.String(), err
}

// detailsDownload 补充一批条目（最多 20 个）的完整信息：播放、点赞、日期、艺人、专辑，音乐再加 Last.fm 重播率与标签。
func (r *Router) detailsDownload(w http.ResponseWriter, req *http.Request, body []byte) {
	var in struct {
		URLs   []string `json:"urls"`
		Scheme string   `json:"scheme"`
	}
	if json.Unmarshal(body, &in) != nil || len(in.URLs) == 0 || len(in.URLs) > 20 {
		http.Error(w, "每次 1–20 个链接", http.StatusBadRequest)
		return
	}
	args := []string{}
	host := ""
	for _, raw := range in.URLs {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			http.Error(w, "不是网址："+raw, http.StatusBadRequest)
			return
		}
		host = u.Hostname()
		args = append(args, u.String())
	}
	if in.Scheme == "music" {
		args = append(args, "--music")
	}
	ctx, cancel := context.WithTimeout(req.Context(), 4*time.Minute)
	defer cancel()
	out, stderrText, err := r.runDownloadScript(ctx, host, "details.py", args...)
	results := []json.RawMessage{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "OMNIHUB_DETAIL "); ok && json.Valid([]byte(rest)) {
			results = append(results, json.RawMessage(rest))
		}
	}
	if len(results) == 0 && err != nil {
		msg := strings.TrimSpace(stderrText)
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		http.Error(w, "补充数据失败："+msg, http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, results)
}
