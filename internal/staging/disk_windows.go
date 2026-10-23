package staging

import (
	"path/filepath"
	"strings"
)

// SameDisk 判断两个路径是否在同一个盘符上。
func SameDisk(a, b string) bool {
	return strings.EqualFold(filepath.VolumeName(a), filepath.VolumeName(b))
}
