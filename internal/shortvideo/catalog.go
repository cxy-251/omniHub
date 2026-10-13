package shortvideo

import "omnihub/internal/catalog"

// VideoCatalog 是通用有界目录缓存的视频适配器。
type VideoCatalog = catalog.Catalog

// ErrNoVideos 表示频道没有可播放视频。
var ErrNoVideos = catalog.ErrNoFiles

// NewVideoCatalog 创建仅收录普通 MP4 文件的按需目录缓存。
func NewVideoCatalog() *VideoCatalog { return catalog.New(".mp4") }
