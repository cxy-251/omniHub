package auth

// TargetFilter 定义端目标隔离规则（第 4.2 节、第 5.2 节）。
type TargetFilter string

const (
	TargetAll     TargetFilter = "all"      // 双端均可见
	TargetGUIOnly TargetFilter = "gui_only" // 仅 Qt 6 本地客户端可见
	TargetWebOnly TargetFilter = "web_only" // 仅 Web 远程浏览器可见
)

// CheckAccess 依据准入公式评估权限：
// 允许访问 = (用户当前等级 >= 分类所需最低等级) ∧ (端目标匹配当前客户端)
func CheckAccess(userLevel int, requiredLevel int, target TargetFilter, clientTarget string) bool {
	// 1. 等级准入判定
	if userLevel < requiredLevel {
		return false
	}

	// 2. 端目标匹配判定
	switch target {
	case TargetGUIOnly:
		return clientTarget == TargetGUI
	case TargetWebOnly:
		return clientTarget == TargetWeb
	default:
		return true
	}
}
