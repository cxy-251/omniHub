// Package tools 实现局域网共享小工具箱（文件传输、剪贴板、诊断与硬件指标）。
// 遵循 ARCHITECTURE.md 第 11 节规范。
package tools

import (
	"sync"
	"time"
)

// ClipboardItem 记录剪贴板单条内容。
type ClipboardItem struct {
	ID        int64     `json:"id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// ClipboardManager 管理局域网跨设备双向剪贴板（第 11.2.2 节）。
type ClipboardManager struct {
	mu      sync.RWMutex
	items   []ClipboardItem
	maxSize int
	nextID  int64
}

// NewClipboardManager 创建剪贴板管理器实例。
func NewClipboardManager(maxSize int) *ClipboardManager {
	if maxSize <= 0 {
		maxSize = 50
	}
	return &ClipboardManager{
		items:   make([]ClipboardItem, 0, maxSize),
		maxSize: maxSize,
		nextID:  1,
	}
}

// Push 存入一条新文本内容。
func (c *ClipboardManager) Push(content string) ClipboardItem {
	c.mu.Lock()
	defer c.mu.Unlock()

	item := ClipboardItem{
		ID:        c.nextID,
		Content:   content,
		CreatedAt: time.Now(),
	}
	c.nextID++

	c.items = append([]ClipboardItem{item}, c.items...)
	if len(c.items) > c.maxSize {
		c.items = c.items[:c.maxSize]
	}
	return item
}

// List 获取全部历史剪贴板条目。
func (c *ClipboardManager) List() []ClipboardItem {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]ClipboardItem, len(c.items))
	copy(res, c.items)
	return res
}

// Clear 清空剪贴板历史。
func (c *ClipboardManager) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make([]ClipboardItem, 0, c.maxSize)
}
