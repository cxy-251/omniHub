// Package sc2 实现星际争霸 2 离线对战参数组装与地图解析。
// 遵循 ARCHITECTURE.md 第 10.1 节规范。
package sc2

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MapItem 记录对战地图元数据。
type MapItem struct {
	Name      string `json:"name"`
	Filename  string `json:"filename"`
	ThumbPath string `json:"thumb_path"`
}

// MatchOptions 封装离线对战开局参数。
type MatchOptions struct {
	MapName    string   `json:"map_name"`
	PlayerRace string   `json:"player_race"` // "P", "T", "Z", "R"
	Difficulty string   `json:"difficulty"`  // "VeryEasy" ~ "CheatInsane"
	Opponents  []string `json:"opponents"`   // 电脑对手种族清单
	Mods       []string `json:"mods"`        // 挂载 Mod 名称
	Launch     bool     `json:"launch"`      // 是否直接拉起游戏进程
}

// ListMaps 扫描指定地图目录下的全部 .SC2Map 文件（第 10.1.2 节）。
func ListMaps(mapsDir string) ([]MapItem, error) {
	if _, err := os.Stat(mapsDir); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(mapsDir)
	if err != nil {
		return nil, err
	}

	var maps []MapItem
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(strings.ToLower(name), ".sc2map") {
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			thumb := filepath.Join(mapsDir, stem+".png")
			if _, err := os.Stat(thumb); err != nil {
				thumb = ""
			}
			maps = append(maps, MapItem{
				Name:      stem,
				Filename:  name,
				ThumbPath: thumb,
			})
		}
	}
	return maps, nil
}

// BuildMatchArgs 根据开局配置动态拼装对战命令行参数（第 10.1.2 节）。
func BuildMatchArgs(opt MatchOptions, sc2Dir string) ([]string, error) {
	if opt.MapName == "" {
		return nil, fmt.Errorf("map name cannot be empty")
	}

	race := opt.PlayerRace
	if race == "" {
		race = "P"
	}

	args := []string{
		"-runtool", "match",
		"-map", opt.MapName,
		"-race", race,
	}

	if opt.Difficulty != "" {
		args = append(args, "-difficulty", opt.Difficulty)
	}

	for _, mod := range opt.Mods {
		if mod != "" {
			args = append(args, "-mod", mod)
		}
	}

	return args, nil
}

// BuildEditorArgs 构造启动银河地图编辑器的命令行参数。
func BuildEditorArgs() []string {
	return []string{"-editor"}
}
