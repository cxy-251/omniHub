package server

import (
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
var retroBiosFiles = map[string]bool{"pgm.zip": true, "pgm2.zip": true, "neogeo.zip": true, "awbios.zip": true}

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
	case strings.HasPrefix(set, "kov3"):
		return "pgm2.zip" // 三国战纪 3：IGS PGM2 二代基板
	case strings.HasPrefix(set, "kov"), strings.HasPrefix(set, "orlegend"), strings.HasPrefix(set, "ddp"), strings.HasPrefix(set, "dmnfrnt"), strings.HasPrefix(set, "martmast"), strings.HasPrefix(set, "photoy2k"), strings.HasPrefix(set, "puzlstar"), strings.HasPrefix(set, "olds"), strings.HasPrefix(set, "killbld"):
		return "pgm.zip"
	case strings.HasPrefix(set, "kof"), strings.HasPrefix(set, "mslug"), strings.HasPrefix(set, "samsh"), strings.HasPrefix(set, "garou"), strings.HasPrefix(set, "rbff"), strings.HasPrefix(set, "fatfur"), strings.HasPrefix(set, "lastblad"), strings.HasPrefix(set, "sengoku"), strings.HasPrefix(set, "blazstar"), strings.HasPrefix(set, "pulstar"), strings.HasPrefix(set, "neobombe"), strings.HasPrefix(set, "aof"), strings.HasPrefix(set, "wh"), strings.HasPrefix(set, "matrim"), strings.HasPrefix(set, "svc"):
		return "neogeo.zip"
	}
	return ""
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
