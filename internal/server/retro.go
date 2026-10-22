package server

import (
	"archive/zip"
	"os"
	"path"
	"strings"
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
