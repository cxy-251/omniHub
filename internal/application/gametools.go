package application

import (
	"os"
	"path/filepath"

	"omnihub/internal/config"
	"omnihub/internal/games"
)

// resolveGameTools 用配置覆盖项，其余按 Steam Deck 上的常见位置查找；找不到的项留空（对应类型启动时报错）。
// Wine 容器优先沿用 omni-deck 的容器，Wine 游戏的存档在里面。
func resolveGameTools(override config.GameTools, configDir string) games.Tools {
	home, _ := os.UserHomeDir()
	first := func(set string, candidates ...string) string {
		if set != "" {
			return set
		}
		for _, c := range candidates {
			if matches, _ := filepath.Glob(c); len(matches) > 0 {
				return matches[len(matches)-1]
			}
		}
		return ""
	}
	tools := games.Tools{
		SteamRoot: first(override.SteamRoot, filepath.Join(home, ".local/share/Steam"), filepath.Join(home, ".steam/steam")),
		Lime3DS:   first(override.Lime3DS, filepath.Join(home, "Applications/Lime3DS/Lime3DS.AppImage")),
		RetroArch: first(override.RetroArch, filepath.Join(home, "Applications/RetroArch/retroarch")),
		RenpySDK:  first(override.RenpySDK, filepath.Join(home, "Applications/renpy-*-sdk/renpy.sh")),
		// SC2 不在资源库里（与 omni-deck 相同的位置）；sc2Mod 是另一个项目，可选。
		SC2Dir:    first(override.SC2Dir, filepath.Join(home, "Games/StarCraft II")),
		SC2ModDir: first(override.SC2ModDir, filepath.Join(home, "Games/claude/omniMod/sc2Mod")),
	}
	// 核心目录：便携版 AppImage 把 HOME 设到旁边的 .home 目录；安装版在 ~/.config/retroarch。
	if tools.RetroArch != "" {
		tools.RetroCores = first(override.RetroCores, filepath.Join(filepath.Dir(tools.RetroArch), "*.AppImage.home/.config/retroarch/cores"), filepath.Join(home, ".config/retroarch/cores"))
	}
	tools.ProtonPrefix = first(override.ProtonPrefix, filepath.Join(home, ".local/share/omni_deck_pfx"))
	if tools.ProtonPrefix == "" {
		tools.ProtonPrefix = filepath.Join(configDir, "proton-prefix")
	}
	return tools
}

// resolveMegaCmd 返回 MEGAcmd 安装目录；配置优先，其次 ~/Applications/mega-cmd，都没有时返回空（用 PATH）。
func resolveMegaCmd(override string) string {
	if override != "" {
		return override
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Applications", "mega-cmd")
	if _, err := os.Stat(filepath.Join(dir, "bin", "mega-exec")); err == nil {
		return dir
	}
	return ""
}
