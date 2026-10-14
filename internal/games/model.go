// Package games 实现游戏中心管理、Proton/Wine 启动调度、看门狗熔断与多用户存档隔离。
// 遵循 ARCHITECTURE.md 第 4 节规范。
package games

import "time"

// Game 记录游戏元数据与硬件画像（对应 games.db 的 games 表）。
type Game struct {
	ID          string `json:"id"`           // 唯一编号主键（如 "001"）
	Name        string `json:"name"`         // 游戏名称
	Category    string `json:"category"`     // 分类（steam, unity, wine, retro, rpg 等）
	Target      string `json:"target"`       // "gui_only" 或 "web_only"（第 4.2 节）
	Icon        string `json:"icon"`         // 图标路径或文件名
	Exe         string `json:"exe"`          // 可执行文件相对路径
	Args        string `json:"args"`         // 启动命令行参数
	Env         string `json:"env"`          // 额外环境变量 (JSON 键值对)
	ProtonAppID string `json:"proton_appid"` // Proton 容器 appid
	RelDir      string `json:"rel_dir"`      // 正斜线目录，相对 standalone_games；旧索引为空

	// 硬件画像监控指标（第 4.1.3 节）
	PeakRSSMB   int64 `json:"peak_rss_mb"`
	PeakIOMBps  int64 `json:"peak_io_mbps"`
	PlayTimeSec int64 `json:"play_time_sec"`
}

// UserGameStat 记录按用户独立统计的游戏游玩时长（第 4.4.4 节）。
type UserGameStat struct {
	UserID       int64     `json:"user_id"`
	GameID       string    `json:"game_id"`
	PlayTimeSec  int64     `json:"play_time_sec"`
	LastPlayedAt time.Time `json:"last_played_at"`
}
