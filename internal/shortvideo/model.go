// Package shortvideo 实现短视频流化、首帧缩略图提取、图集封装与跨设备融合。
// 遵循 ARCHITECTURE.md 第 2 节规范。
package shortvideo

import "time"

// Creator 记录博主信息。
type Creator struct {
	ID        string `json:"id"`        // 博主唯一标识（由博主文件夹名生成）
	Name      string `json:"name"`      // 博主名称
	Platform  string `json:"platform"`  // 所属平台（快手/抖音/TikTok）
	ItemCount int    `json:"item_count"`// 作品总数
}

// Post 记录作品条目（视频或图集）。
type Post struct {
	ID        string    `json:"id"`        // 作品 ID
	CreatorID string    `json:"creator_id"`// 所属博主 ID
	Title     string    `json:"title"`     // 作品标题
	Type      string    `json:"type"`      // "video" 或 "album"
	RelPath   string    `json:"rel_path"`  // 相对博主目录的文件路径
	CreatedAt time.Time `json:"created_at"`
}

// AlbumImage 记录图集内单张图片（存储于 albums.db）。
type AlbumImage struct {
	PostID     string `json:"post_id"`
	OrderIndex int    `json:"order_index"` // 自然排序序号 (1, 2, 3...)
	Data       []byte `json:"-"`           // 图片原图二进制
}

// Thumbnail 记录视频首帧或图集缩略图（存储于 thumbnails.db）。
type Thumbnail struct {
	TargetID string `json:"target_id"` // post_id
	Data     []byte `json:"-"`
}
