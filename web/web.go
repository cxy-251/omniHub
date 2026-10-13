// Package web 提供内嵌前端静态资产。
package web

import "embed"

// FS 编译嵌入的静态网页资源文件系统。
//
//go:embed index.html css js vendor players
var FS embed.FS
