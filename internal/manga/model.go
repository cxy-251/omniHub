// Package manga 实现漫画专区 CBZ 零落盘流化、元数据解析与条漫断点记忆。
// 遵循 ARCHITECTURE.md 第 7 节规范。
package manga

import "time"

// Manga 记录单部漫画元数据。
type Manga struct {
	ID             string    `json:"id"`              // 漫画唯一 ID（数字 ID 或文件名哈希）
	Title          string    `json:"title"`           // 漫画标题
	Author         string    `json:"author"`          // 作者
	Description    string    `json:"description"`     // 简介
	Tags           []string  `json:"tags"`            // 标签列表
	ChapterCount   int       `json:"chapter_count"`   // 章节总数
	TotalPages     int       `json:"total_pages"`     // 总页数
	IsComplete     bool      `json:"is_complete"`     // 是否已完结
	RelPath        string    `json:"rel_path"`        // 相对 manga 目录路径
	CustomCover    string    `json:"custom_cover"`    // 手动指定的封面包内条目相对路径（第 7.2.3 节）
	UpdatedAt      time.Time `json:"updated_at"`
}

// Chapter 记录单话章节。
type Chapter struct {
	Index      int      `json:"index"`       // 章节序号 (1, 2, 3...)
	Title      string   `json:"title"`       // 章节名称
	PageCount  int      `json:"page_count"`  // 本章页数
	PageFiles  []string `json:"page_files"`  // 本章内部图片条目相对路径
}

// MangaProgress 记录用户在条漫模式下的阅读进度（第 7.3.2 节）。
type MangaProgress struct {
	UserID            int64     `json:"user_id"`
	MangaID           string    `json:"manga_id"`
	ChapterIndex      int       `json:"chapter_index"`
	PageIndex         int       `json:"page_index"`
	ScrollOffsetRatio float64   `json:"scroll_offset_ratio"` // 0.0 ~ 1.0 像素级垂直相对比例
	UpdatedAt         time.Time `json:"updated_at"`
}
