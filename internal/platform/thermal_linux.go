package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// 温度传感器：优先用电池温度（手摸到的冷热、对电池寿命要紧的都是它）；没有电池传感器时用机身的 quiet_therm。
// quiet_therm 在主板上，持续工作时比电池高 2°C 左右（2026-10-26 手机实测 32.5 / 30.4°C），
// 以前两者取高，机身 40°C 就暂停，电池其实才 38°C 左右，手机摸着并不热。Deck 上没有这两个传感器。
var thermalZone = sync.OnceValue(func() string {
	found := map[string]string{}
	dirs, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, d := range dirs {
		if t, err := os.ReadFile(filepath.Join(d, "type")); err == nil {
			found[strings.TrimSpace(string(t))] = filepath.Join(d, "temp")
		}
	}
	if z := found["battery"]; z != "" {
		return z
	}
	return found["quiet_therm"]
})

// DeviceTemp 返回机身温度（°C，见 thermalZone）；读不到时 ok 为 false。
func DeviceTemp() (temp float64, ok bool) {
	z := thermalZone()
	if z == "" {
		return 0, false
	}
	data, err := os.ReadFile(z)
	if err != nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return float64(v) / 1000, true
}
