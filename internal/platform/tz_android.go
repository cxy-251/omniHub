package platform

import (
	"os"
	"os/exec"
	"strings"
	"time"
)

// 安卓上没设 TZ 时 Go 程序按 UTC 计时（手机 Termux 实测日志差 8 小时），这里按系统设置的时区改过来。
func init() {
	if os.Getenv("TZ") != "" {
		return
	}
	out, err := exec.Command("/system/bin/getprop", "persist.sys.timezone").Output()
	if err != nil {
		return
	}
	if loc, err := time.LoadLocation(strings.TrimSpace(string(out))); err == nil {
		time.Local = loc
	}
}
