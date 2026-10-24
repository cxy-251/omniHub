package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
	Listable    bool     `json:"listable"` // 可以先「读取列表」看会下什么、勾选后再下
	// ModeDestinations：按模式下到哪里（有「模式」字段的方案）
	ModeDestinations map[string]string `json:"mode_destinations,omitempty"`
}

func archivePath(root, scheme string) string {
	return filepath.Join(root, ".omni_data", "download-archive-"+scheme+".txt")
}

func (r *Router) schemeKinds() []*dlKind {
	libs, def := r.stagingLibraryChoice()
	library := dlField{Name: "library", Label: "暂存到", Type: "choice", Options: libs, Default: def, Help: "暂存区在资源库里；移入时同一块盘上只是改名"}
	height := dlField{Name: "max_height", Label: "最高分辨率", Type: "choice", Default: "1080", Options: []dlOption{{"2160", "4K"}, {"1440", "1440p"}, {"1080", "1080p"}, {"720", "720p"}, {"480", "480p"}, {"0", "不限"}}}
	urls := dlField{Name: "urls", Label: "链接", Type: "lines", Required: true}
	// YouTube 只选模式，挑什么、怎么排、放哪里、画质都由模式决定（见 general.py 的 list_videos）
	mode := dlField{Name: "mode", Label: "模式", Type: "modes", Default: "video", Options: []dlOption{
		{"girls", "女团 MV：只要女团的正式 MV，按公司 / 团分，4K"},
		{"audio", "有声书 / 音频：只要声音，按列表顺序，放进音声"},
		{"video", "普通视频：按发布者分"},
	}}
	return []*dlKind{
		{
			ID: "music", Title: "音乐", Description: "一次可以填很多个歌单、专辑、单曲或歌手页（YouTube、YouTube Music、B 站等），全部下载，不筛选。YouTube 的歌换成 YouTube Music 上的纯音轨版（MV 有片头片尾，不下它的音频）；找不到纯音轨版时只收官方音频（Official Audio），MV、歌词视频、现场、翻唱都不下。按歌手归档：属于专辑、EP 的歌放进歌手下的专辑文件夹，单曲放在歌手文件夹里。歌手页（YouTube Music 歌手页或歌手的官方频道）下全部专辑、EP、单曲，同一首只留最早发行的那版，MV（标题写着 Music Video、Official Video、MV 的，歌词视频、现场不算）下成视频放进短视频的 MV/<歌手>。只取最好的音轨，不转码，嵌入封面和标签。每首的播放数、点赞数、发布日期、下载日期，以及 Last.fm 的风格、听众、总播放、重播率写进文件标签，播放时显示。下过的自动跳过。",
			Scheme: &dlScheme{Listable: true, Steps: []string{"粘贴歌单/专辑/歌手链接（一行一个）", "读取列表：看会下哪些歌和 MV、哪些不下，去掉不要的勾", "下载到暂存区（下过的跳过）", "播放数、点赞、日期、Last.fm 信息写进标签", "在「暂存区」试听、删掉不要的", "移入资源库"}, Destination: "音声 / 音乐 / <歌手>/（整张专辑放 <歌手>/<专辑>/）",
				Input: "歌单、专辑、单曲或歌手页链接，一行一个", Placeholder: "https://music.youtube.com/playlist?list=LM（赞过的歌）\nhttps://music.youtube.com/channel/…（歌手）\nhttps://www.youtube.com/watch?v=…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, library},
			build:  func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("music", p) },
		},
		{
			ID: "youtube", Title: "YouTube", Description: "一次可以填很多个频道、播放列表或视频，选一个模式：女团 MV 模式只留正式 MV（去掉预告、练习室、舞台、花絮、反应视频），从标题认出艺人，按 Wikidata 只留女团（男团、个人歌手去掉，查不出的单独列出），放进「公司频道 / 团名」，有 4K 就要（和音乐方案歌手页下的欧美 MV 是两类）；有声书模式只下声音，按列表顺序放进音声「书名」专辑（书名默认用播放列表名，可以改）；普通视频放进发布者文件夹。先「读取列表」看会下哪些、按组勾选，再一个一个下载。下过的自动跳过。",
			Scheme: &dlScheme{Steps: []string{"粘贴频道/播放列表/视频链接（一行一个）", "选模式", "读取列表，勾选要的", "一个一个下载到暂存区（下过的跳过）", "移入资源库"},
				Destination: "短视频 / YouTube / <发布者>/", Listable: true,
				ModeDestinations: map[string]string{"girls": "短视频 / YouTube / <公司频道>/<团名>/", "audio": "音声 / <书名>/", "video": "短视频 / YouTube / <发布者>/"},
				Input:            "频道、播放列表或视频链接，一行一个", Placeholder: "https://www.youtube.com/@HYBELABELS\nhttps://www.youtube.com/playlist?list=…\nhttps://www.youtube.com/watch?v=…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, mode, library},
			build:  func(r *Router, p dlParams) (dlPlan, error) { return r.buildScheme("youtube", p) },
		},
		{
			ID: "bilibili", Title: "B 站", Description: "一次可以填很多个 UP 主、合集或视频，正常下载视频，放进 UP 主文件夹。先「读取列表」看会下哪些（按播放量排），勾选后一个一个下载。下过的自动跳过。",
			Scheme: &dlScheme{Steps: []string{"粘贴 UP 主空间/合集/视频链接（一行一个）", "读取列表，勾选要的", "一个一个下载到暂存区（下过的跳过）", "移入资源库"},
				Destination: "短视频 / B站 / <UP主>/", Listable: true,
				Input: "UP 主空间、合集或视频链接，一行一个", Placeholder: "https://space.bilibili.com/<UID>/video\nhttps://www.bilibili.com/video/BV…"},
			needs:  []string{"uv", "ffmpeg", "ffprobe"},
			Fields: []dlField{urls, library},
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

var (
	planToken = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
	videoIDs  = regexp.MustCompile(`^[A-Za-z0-9_,-]*$`)
)

// planPath 是音乐「读取列表」写出的列表文件（界面生成的令牌对应一个文件）。
func (r *Router) planPath(token string) (string, bool) {
	if !planToken.MatchString(token) {
		return "", false
	}
	return filepath.Join(r.cacheDir, "download-plans", token+".json"), true
}

// buildList 是「读取列表」：脚本只展开链接、筛选（音乐还换纯音轨版、查专辑），把会下什么写进列表文件，不下载、不建暂存批次。
func (r *Router) buildList(scheme string, p dlParams, urls []string) (dlPlan, error) {
	path, ok := r.planPath(p.text("plan_token"))
	if !ok {
		return dlPlan{}, errors.New("缺少列表令牌")
	}
	lib, err := r.writableLibrary(p.text("library"))
	if err != nil {
		return dlPlan{}, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return dlPlan{}, err
	}
	// 一周前的列表删掉
	if old, err := os.ReadDir(dir); err == nil {
		for _, e := range old {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 7*24*time.Hour {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	args := append([]string{"scripts/general.py"}, urls...)
	args = append(args, "--batch", dir, "--scheme", scheme, "--archive", archivePath(lib.RootPath, scheme), "--plan-out", path)
	args = append(args, videoArgs(p)...)
	titles := map[string]string{"music": "音乐", "youtube": "YouTube", "bilibili": "B站"}
	return dlPlan{title: fmt.Sprintf("%s 读取列表 %d 个链接", titles[scheme], len(urls)), args: args, resource: strings.TrimSuffix(path, ".json"), lib: lib}, nil
}

// videoArgs 是视频方案的模式与书名（读取列表和下载共用）。
func videoArgs(p dlParams) []string {
	var args []string
	if m := p.text("mode"); m == "video" || m == "girls" || m == "audio" {
		args = append(args, "--mode", m)
	}
	return args
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
	if p.text("step") == "list" && (scheme == "music" || scheme == "youtube" || scheme == "bilibili") {
		return r.buildList(scheme, p, urls)
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
	if token := p.text("plan"); token != "" { // 按读取好的列表下，去掉没勾的
		path, ok := r.planPath(token)
		if !ok || !videoIDs.MatchString(p.text("exclude")) {
			return dlPlan{}, errors.New("列表参数不对")
		}
		if _, err := os.Stat(path); err != nil {
			return dlPlan{}, errors.New("列表已过期，请重新读取")
		}
		args = append(args, "--plan", path, "--exclude", p.text("exclude"))
	}
	if scheme == "youtube" || scheme == "bilibili" { // 直接下载与按列表下载都要：模式
		args = append(args, videoArgs(p)...)
	} else if p.text("mode") == "direct" {
		if rank := p.text("rank"); rank != "" {
			args = append(args, "--rank", rank, "--top", p.text("top"))
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
