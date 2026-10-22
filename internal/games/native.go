package games

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// 本地客户端（gui_only）的游戏分类：standalone_games/<分类>_games。
// 网页游戏（rpg、slg、retro）只在 Web 端；Flash 不再支持。
var NativeCategories = []string{"steam", "unity", "wine", "godot", "unreal", "renpy", "3ds", "app"}

// Meta 是游戏目录下 omni.json 的内容，全部字段可选（与 omni-deck 兼容）。
type Meta struct {
	Name        string            `json:"name"`
	Hidden      bool              `json:"hidden"`
	Icon        string            `json:"icon"`
	Exe         string            `json:"exe"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	ProtonAppID json.RawMessage   `json:"proton_appid"` // 字符串或数字
}

// ReadMeta 读取 omni.json；不存在或格式错误时返回空 Meta。
func ReadMeta(gameDir string) Meta {
	var m Meta
	f, err := os.Open(filepath.Join(gameDir, "omni.json"))
	if err != nil {
		return m
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

// ProtonApp 返回 proton_appid 的字符串形式（只允许数字）。
func (m Meta) ProtonApp() string {
	s := strings.Trim(string(m.ProtonAppID), `" `)
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

// Entry 是大厅里的一个游戏。ID 为「分类/目录名」，在同一资源库内唯一。
type Entry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	Folder    string `json:"folder"`
	Icon      string `json:"icon,omitempty"` // 相对游戏目录
	LibraryID string `json:"library_id"`
}

var iconCandidates = []string{"icon.png", "cover.png", "cover.jpg", "icon.jpg", "icon/icon.png", "game/gui/window_icon.png", "www/icon/icon.png"}

// ScanCategory 列出一个分类目录下的游戏（跳过隐藏目录与 omni.json hidden）。只读取目录与少量 stat，不遍历游戏内容。
func ScanCategory(categoryDir, category, libraryID string) ([]Entry, error) {
	entries, err := os.ReadDir(categoryDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if category == "retro" && e.Name() == "bios" {
			continue // 模拟器 BIOS 存放处，不是游戏
		}
		dir := filepath.Join(categoryDir, e.Name())
		meta := ReadMeta(dir)
		if meta.Hidden {
			continue
		}
		entry := Entry{ID: category + "/" + e.Name(), Name: e.Name(), Category: category, Folder: e.Name(), LibraryID: libraryID}
		if meta.Name != "" {
			entry.Name = meta.Name
		}
		icons := iconCandidates
		if meta.Icon != "" {
			icons = append([]string{meta.Icon}, icons...)
		}
		for _, c := range icons {
			if filepath.IsLocal(c) {
				if info, err := os.Stat(filepath.Join(dir, c)); err == nil && info.Mode().IsRegular() {
					entry.Icon = filepath.ToSlash(c)
					break
				}
			}
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Folder < out[j].Folder })
	return out, nil
}

// 自动挑选主程序时排除的文件名片段（安装器、崩溃上报、运行库、启动器、专用服务器等）。
var badExeWords = []string{"crashhandler", "crashpad", "crashreport", "reipatcher", "setup", "uninstall", "ueprereqsetup", "config",
	"エンジン設定", "vcredist", "dxredist", "redist", "directx", "elevate", "oalinst", "openal", "dotnet", "vc_redist", "unitycrash",
	"dowser", "dedicated_server", "nullrenderer"}

// 常见大作的主程序名（omni-deck 积累的表），同目录有多个候选时直接选中。
var knownMainExes = map[string]bool{
	"oxygennotincluded": true, "dontstarve": true, "dontstarve_steam": true, "dontstarve_steam_x64.exe": true, "dspgame.exe": true,
	"deadcells.exe": true, "factorio.exe": true, "davethediver.exe": true, "descenders.exe": true, "rimworldwin64.exe": true,
	"vampiresurvivors.exe": true, "thronefall.exe": true, "sandustry.exe": true, "bloonstd6.exe": true, "cult of the lamb.exe": true,
	"eurotrucks2.exe": true, "hades2.exe": true, "hoi4.exe": true, "silksong.exe": true, "hollow_knight.exe": true, "nms.exe": true,
	"palworld.exe": true, "shapez2.exe": true, "stellaris.exe": true,
}

// FindExecutable 返回游戏主程序（相对游戏目录）。omni.json 的 exe 优先；否则按 omni-deck 的规则打分：
// 原生 Linux 程序、中文版、便携版加分，目录越深越减分。3DS 找 ROM 镜像，Ren'Py 优先启动脚本。
func FindExecutable(gameDir, category string, meta Meta) (string, error) {
	if meta.Exe != "" {
		if !filepath.IsLocal(meta.Exe) {
			return "", errors.New("omni.json exe must be relative to the game directory")
		}
		return filepath.ToSlash(meta.Exe), nil
	}
	type candidate struct {
		score int
		rel   string
	}
	var best *candidate
	consider := func(score int, rel string) {
		if best == nil || score > best.score || score == best.score && rel < best.rel {
			best = &candidate{score, rel}
		}
	}
	maxDepth := 3
	if category == "renpy" || category == "3ds" || category == "app" {
		maxDepth = 2
	}
	visited := 0
	err := filepath.WalkDir(gameDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(gameDir, path)
		depth := strings.Count(rel, string(filepath.Separator))
		if d.IsDir() {
			if path != gameDir && (strings.HasPrefix(d.Name(), ".") || depth >= maxDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if visited++; visited > 20000 {
			return filepath.SkipAll
		}
		name := strings.ToLower(d.Name())
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		switch category {
		case "3ds":
			if strings.HasSuffix(name, ".cci") || strings.HasSuffix(name, ".3ds") || strings.HasSuffix(name, ".cxi") {
				consider(100-depth*10, relSlash)
			}
			return nil
		case "renpy":
			switch {
			case containsAny(name, "crashhandler", "oalinst", "vcredist", "dxsetup"):
			case strings.HasSuffix(name, ".sh"):
				consider(100-depth*10, relSlash)
			case strings.HasSuffix(name, ".py") && name != "game.py":
				consider(80-depth*10, relSlash)
			case strings.HasSuffix(name, ".exe"):
				consider(50-depth*10, relSlash)
			}
			return nil
		}
		native := strings.HasSuffix(name, ".x86_64") || strings.HasSuffix(name, ".x86") || strings.HasSuffix(name, ".sh") ||
			(!strings.Contains(name, ".") && info.Mode().Perm()&0111 != 0)
		if !native && !strings.HasSuffix(name, ".exe") || containsAny(name, badExeWords...) {
			return nil
		}
		score := 100 - depth*15
		if native {
			score += 60
		}
		if containsAny(name, "_cn", "chs", "chinese", "中文") {
			score += 50
		}
		if strings.Contains(name, "portable") {
			score += 40
		}
		if strings.HasSuffix(name, ".exe") {
			score += 10
		}
		if strings.Contains(name, "loader") {
			score -= 30
		}
		if strings.Contains(name, "_gl.exe") {
			score -= 20
		}
		if strings.HasSuffix(name, "-shipping.exe") {
			score += 80 // 虚幻引擎发行版主程序
		}
		if knownMainExes[name] {
			score += 100
		}
		consider(score, relSlash)
		return nil
	})
	if err != nil {
		return "", err
	}
	if best == nil {
		return "", errors.New("no game executable found; set \"exe\" in omni.json")
	}
	return best.rel, nil
}

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// Tools 是启动非原生游戏所需的外部运行时位置（由配置提供，留空表示不可用）。
type Tools struct {
	SteamRoot    string // 含 steamapps/common/Proton* 与 steamapps/compatdata
	ProtonPrefix string // 默认 Wine 容器（存档在里面，沿用 omni-deck 的容器）
	Lime3DS      string // Lime3DS 可执行文件（AppImage）
	RenpySDK     string // renpy.sh
	SC2Dir       string // 星际争霸 2 安装目录（含 Maps/）
	SC2ModDir    string // omniMod 的 sc2Mod 项目（bake.py，可选）
}

// 按优先级尝试的 Proton 版本目录名。
var protonNames = []string{"Proton - Experimental", "Proton 11.0", "Proton 10.0", "Proton 9.0 (Beta)", "Proton 8.0", "Proton 7.0", "Proton 5.13"}

// Command 生成启动命令（argv 与追加环境变量）。exe 为相对游戏目录的主程序。
func Command(gameDir, category, exe string, meta Meta, tools Tools) (argv, env []string, err error) {
	full := filepath.Join(gameDir, filepath.FromSlash(exe))
	lower := strings.ToLower(exe)
	switch {
	case category == "3ds" && !strings.HasSuffix(lower, ".sh") && !strings.HasSuffix(lower, ".appimage"):
		if tools.Lime3DS == "" {
			return nil, nil, errors.New("Lime3DS emulator is not configured")
		}
		argv = []string{tools.Lime3DS, full}
	case strings.HasSuffix(lower, ".exe") && runtime.GOOS != "windows":
		proton := ""
		for _, name := range protonNames {
			p := filepath.Join(tools.SteamRoot, "steamapps", "common", name, "proton")
			if _, err := os.Stat(p); err == nil && tools.SteamRoot != "" {
				proton = p
				break
			}
		}
		if proton == "" {
			return nil, nil, errors.New("no Proton found under the configured Steam root")
		}
		prefix := tools.ProtonPrefix
		if app := meta.ProtonApp(); app != "" {
			// 有专属 compatdata 的游戏用它自己的容器（例如从 Steam 安装过的游戏）。
			if p := filepath.Join(tools.SteamRoot, "steamapps", "compatdata", app); dirExists(p) {
				prefix = p
			}
		}
		if prefix == "" {
			return nil, nil, errors.New("no Wine prefix configured")
		}
		if err := os.MkdirAll(prefix, 0755); err != nil {
			return nil, nil, err
		}
		argv = []string{proton, "run", full}
		env = []string{"STEAM_COMPAT_CLIENT_INSTALL_PATH=" + tools.SteamRoot, "STEAM_COMPAT_DATA_PATH=" + prefix, "WINEDEBUG=-all"}
		if category == "unreal" {
			// UE5 在 Proton 下常见 "FATAL: exception not rethrown"，关闭崩溃上报并启用异步着色器。
			env = append(env, "WINE_LARGE_ADDRESS_AWARE=1", "DXVK_ASYNC=1", "UE_DISABLE_CRASH_REPORTER=1", "UE_NO_CRASH_REPORT=1")
		}
	case category == "renpy" && !strings.HasSuffix(lower, ".sh") && !strings.HasSuffix(lower, ".py") && tools.RenpySDK != "":
		argv = []string{tools.RenpySDK, gameDir}
	default:
		// 原生程序：素材盘上常缺执行位，与 omni-deck 一样补上。
		if info, err := os.Stat(full); err == nil && info.Mode().Perm()&0100 == 0 {
			_ = os.Chmod(full, info.Mode().Perm()|0111)
		}
		argv = []string{full}
	}
	argv = append(argv, meta.Args...)
	for k, v := range meta.Env {
		if k != "" && !strings.ContainsAny(k, "=\x00") && !strings.Contains(v, "\x00") {
			env = append(env, k+"="+v)
		}
	}
	env = append(env, "LANG=zh_CN.UTF-8", "LC_ALL=zh_CN.UTF-8")
	return argv, env, nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
