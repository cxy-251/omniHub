// Package gallery 实现相册画廊的存储、冷热分离、分卷归档与读取引擎。
// 遵循 ARCHITECTURE.md 第 3 节规范。
package gallery

import (
	"errors"
	"time"
)

// ErrImageCapacity reports a BLOB beyond the bounded thumbnail/original read budget.
var ErrImageCapacity = errors.New("image exceeds read capacity")

// 单分卷最大容量上限：4GB（第 3.1.2 节、第 3.3.2 节）
const MaxVolumeSizeBytes = 4 * 1024 * 1024 * 1024

// Photo 记录单张照片的元数据与状态（对应 gallery.db 的 photos 表）。
type Photo struct {
	ID          string     `json:"id"`           // SHA-256 内容哈希
	Filename    string     `json:"filename"`     // 原始文件名
	SizeBytes   int64      `json:"size_bytes"`   // 原始字节数
	MimeType    string     `json:"mime_type"`    // 如 image/jpeg, image/png
	Width       int        `json:"width"`        // 物理像素宽度
	Height      int        `json:"height"`       // 物理像素高度
	Volume      string     `json:"volume"`       // 存放分卷名，如 originals_vol01.db
	ColorFamily int        `json:"color_family"` // 色系编号 0~9
	ColorHex    string     `json:"color_hex"`    // 主色 HEX，如 #1A2B3C
	IsFavorite  bool       `json:"is_favorite"`  // 收藏标记
	CreatedAt   time.Time  `json:"created_at"`   // 创建/拍摄时间
	DeletedAt   *time.Time `json:"deleted_at"`   // 软删除标记（废纸篓，第 3.3.3 节）
}

// Thumbnail 记录 480px 黄金规格缩略图（对应 gallery.db 的 thumbnails 表）。
type Thumbnail struct {
	PhotoID  string `json:"photo_id"`
	SizeType string `json:"size_type"` // 固定为 "m"
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Data     []byte `json:"-"`
}
