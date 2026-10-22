package server

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"omnihub/internal/games"
)

// 复古游戏的机种与街机 BIOS。
// 机种看游戏目录名末尾的括号（如 "Metal Slug X (PS1)"），没有时由播放页按 ROM 扩展名判断。
// 街机 BIOS（pgm.zip、neogeo.zip 等第三方固件，不进仓库）放在资源库 standalone_games/retro_games/bios/，
// 只通过该游戏的授权地址 /game-content/<token>/__bios/<名字> 提供。
var retroSystems = map[string]string{
	"gb": "gb", "gbc": "gb", "gba": "gba", "nds": "nds", "n64": "n64", "nes": "nes", "fc": "nes",
	"snes": "snes", "sfc": "snes", "ps1": "psx", "psx": "psx", "arcade": "arcade",
	"md": "segaMD", "genesis": "segaMD", "sms": "segaMS", "gg": "segaGG", "pce": "pce",
}

// retroBiosFiles 是允许提供的 BIOS 文件名。
var retroBiosFiles = map[string]bool{"pgm.zip": true, "neogeo.zip": true, "awbios.zip": true}

func retroSystem(folder string) string {
	open := strings.LastIndex(folder, "(")
	if open < 0 || !strings.HasSuffix(folder, ")") {
		return ""
	}
	return retroSystems[strings.ToLower(strings.TrimSpace(folder[open+1:len(folder)-1]))]
}

// retroBios 按街机 romset 名（ROM 文件名去掉扩展名）判断需要的 BIOS；不需要时为空。
func retroBios(system, entry string) string {
	if system != "arcade" {
		return ""
	}
	set := strings.ToLower(strings.TrimSuffix(path.Base(entry), path.Ext(entry)))
	switch {
	case retroUnsupported(system, entry):
		return ""
	case strings.HasPrefix(set, "kov"), strings.HasPrefix(set, "orlegend"), strings.HasPrefix(set, "ddp"), strings.HasPrefix(set, "dmnfrnt"), strings.HasPrefix(set, "martmast"), strings.HasPrefix(set, "photoy2k"), strings.HasPrefix(set, "puzlstar"), strings.HasPrefix(set, "olds"), strings.HasPrefix(set, "killbld"):
		return "pgm.zip"
	case strings.HasPrefix(set, "kof"), strings.HasPrefix(set, "mslug"), strings.HasPrefix(set, "samsh"), strings.HasPrefix(set, "garou"), strings.HasPrefix(set, "rbff"), strings.HasPrefix(set, "fatfur"), strings.HasPrefix(set, "lastblad"), strings.HasPrefix(set, "sengoku"), strings.HasPrefix(set, "blazstar"), strings.HasPrefix(set, "pulstar"), strings.HasPrefix(set, "neobombe"), strings.HasPrefix(set, "aof"), strings.HasPrefix(set, "wh"), strings.HasPrefix(set, "matrim"), strings.HasPrefix(set, "svc"):
		return "neogeo.zip"
	}
	return ""
}

// retroUnsupported 报告网页模拟器跑不了的街机基板：IGS PGM2（三国战纪 3、西游释厄传 2 等）。
// 网页端的街机核心是 FBNeo，它没有 PGM2 驱动，补什么 BIOS 都没用（PGM2 的固件本来就在各游戏自己的 ROM 包里）。
func retroUnsupported(system, entry string) bool {
	if system != "arcade" {
		return false
	}
	set := strings.ToLower(strings.TrimSuffix(path.Base(entry), path.Ext(entry)))
	for _, prefix := range []string{"kov3", "orleg2", "kov2nl", "ddpdoj", "kof98umh", "bubbub"} {
		if strings.HasPrefix(set, prefix) {
			return true
		}
	}
	return false
}

// 桌面端各机种按顺序尝试的 RetroArch 核心（取第一个已装的）。
var retroDesktopCores = map[string][]string{
	"gb": {"gambatte", "mgba", "sameboy"}, "gba": {"mgba", "vba_next"}, "nes": {"fceumm", "nestopia", "mesen"},
	"snes": {"snes9x", "bsnes"}, "n64": {"mupen64plus_next", "parallel_n64"}, "nds": {"melondsds", "melonds", "desmume"},
	"psx": {"pcsx_rearmed", "swanstation", "mednafen_psx_hw"}, // PCSX-ReARMed 自带 HLE BIOS "segaMD": {"genesis_plus_gx", "picodrive"},
	"segaMS": {"genesis_plus_gx", "picodrive"}, "segaGG": {"genesis_plus_gx"}, "pce": {"mednafen_pce_fast", "mednafen_pce"},
	"arcade": {"fbneo", "mame"},
}

// 没有机种标记时按 ROM 扩展名判断（与网页播放页相同）。
var retroExtensionSystems = map[string]string{
	".nes": "nes", ".sfc": "snes", ".smc": "snes", ".gb": "gb", ".gbc": "gb", ".gba": "gba", ".n64": "n64", ".z64": "n64", ".v64": "n64",
	".nds": "nds", ".md": "segaMD", ".gen": "segaMD", ".sms": "segaMS", ".gg": "segaGG", ".pce": "pce", ".zip": "arcade",
}

// retroCommand 生成桌面端用 RetroArch 运行模拟器游戏的命令。PGM 一、二代街机用 MAME 核心：
// FBNeo 没有 PGM2 驱动，而 PGM 一代要的是 FBNeo 版 BIOS 包，MAME 用普通的 pgm.zip 就能跑。
// 其余街机用 FBNeo，系统目录指向资源库的 BIOS 目录（neogeo.zip 等）。
func (r *Router) retroCommand(libID, dir string, meta games.Meta) (string, []string, error) {
	tools := r.gameTools
	if tools.RetroArch == "" || tools.RetroCores == "" {
		return "", nil, errors.New("没有找到 RetroArch（默认位置 ~/Applications/RetroArch/retroarch，或在配置 game_tools.retroarch 里指定）")
	}
	_, entry, err := games.WebEntry(dir, &games.Game{Target: "web_only", Category: "retro", Exe: meta.Exe})
	if err != nil {
		return "", nil, err
	}
	system := retroSystem(filepath.Base(dir))
	if system == "" {
		system = retroExtensionSystems[strings.ToLower(path.Ext(entry))]
	}
	cores := retroDesktopCores[system]
	if system == "arcade" && (retroUnsupported(system, entry) || retroBios(system, entry) == "pgm.zip") {
		cores = []string{"mame"}
	}
	if len(cores) == 0 {
		return "", nil, fmt.Errorf("认不出「%s」的机种", filepath.Base(dir))
	}
	core := ""
	for _, name := range cores {
		if p := filepath.Join(tools.RetroCores, name+"_libretro.so"); fileExists(p) {
			core = p
			break
		}
	}
	if core == "" {
		return "", nil, fmt.Errorf("RetroArch 缺少核心 %s：在 RetroArch 主菜单「在线更新 → 核心下载」里安装后再打开", cores[0])
	}
	// 按 RetroArch 自己的设置开窗口（默认窗口模式，游戏里按 F 切换全屏），不强制全屏。
	argv := []string{tools.RetroArch, "-L", core}
	if bios, err := r.libMgr.ResolveCategoryPath(libID+":standalone_games/retro_games/bios", "standalone_games/retro_games"); err == nil && filepath.Base(core) == "fbneo_libretro.so" {
		// 只对这次启动生效的追加配置：系统目录（BIOS）指向资源库。
		cfg := filepath.Join(r.cacheDir, "retroarch-"+libID+".cfg")
		if err := os.WriteFile(cfg, []byte(fmt.Sprintf("system_directory = %q\n", bios)), 0o600); err == nil {
			argv = append(argv, "--appendconfig="+cfg)
		}
	}
	return entry, append(argv, filepath.Join(dir, filepath.FromSlash(entry))), nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// retroBiosNeeds 是网页街机核心 FBNeo 对 BIOS 包内文件的要求。FBNeo 的 pgm.zip 比 MAME 的多两个文件，
// 拿 MAME 版的 pgm.zip 打开任何 PGM 游戏都会报 "ddp3_bios.u37 ... is required"。
var retroBiosNeeds = map[string][]string{
	"pgm.zip": {"pgm_t01s.rom", "pgm_m01s.rom", "pgm_p01s.u20", "ddp3_bios.u37", "bios.u42"},
}

// retroBiosLacking 列出 BIOS 包里缺的文件（按文件名，不分大小写）；读不了包时当作都不缺，交给模拟器报错。
func retroBiosLacking(file, name string) []string {
	archive, err := zip.OpenReader(file)
	if err != nil {
		return nil
	}
	defer archive.Close()
	have := map[string]bool{}
	for _, f := range archive.File {
		have[strings.ToLower(path.Base(f.Name))] = true
	}
	var lacking []string
	for _, need := range retroBiosNeeds[name] {
		if !have[need] {
			lacking = append(lacking, need)
		}
	}
	return lacking
}

// retroBiosPath 返回某个库里 BIOS 文件的路径（存在时）。
func (r *Router) retroBiosPath(libID, name string) (string, bool) {
	if !retroBiosFiles[name] {
		return "", false
	}
	dir, err := r.libMgr.ResolveCategoryPath(libID+":standalone_games/retro_games/bios", "standalone_games/retro_games")
	if err != nil {
		return "", false
	}
	p := dir + "/" + name
	info, err := os.Stat(p)
	return p, err == nil && info.Mode().IsRegular()
}
