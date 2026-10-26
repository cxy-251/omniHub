package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var thermalZones = sync.OnceValue(func() []string {
	var out []string
	dirs, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, d := range dirs {
		t, err := os.ReadFile(filepath.Join(d, "type"))
		// 机身与电池温度：安卓手机上普通应用（Termux）也能读；Deck 上没有这两个传感器
		if err == nil && (strings.TrimSpace(string(t)) == "quiet_therm" || strings.TrimSpace(string(t)) == "battery") {
			out = append(out, filepath.Join(d, "temp"))
		}
	}
	return out
})

// DeviceTemp 返回机身温度（°C，机身与电池传感器取较高者）；读不到时 ok 为 false。
func DeviceTemp() (temp float64, ok bool) {
	for _, z := range thermalZones() {
		data, err := os.ReadFile(z)
		if err != nil {
			continue
		}
		if v, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if t := float64(v) / 1000; !ok || t > temp {
				temp, ok = t, true
			}
		}
	}
	return temp, ok
}
