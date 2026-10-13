//go:build !linux && !darwin && !windows

package platform

import "errors"

// RenameNoReplace 在尚未支持的平台明确拒绝迁移，不降级为可能覆盖的重命名。
func RenameNoReplace(source, destination string) error {
	return errors.New("exclusive state rename unsupported on this platform")
}
