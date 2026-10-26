//go:build !linux

package platform

// DeviceTemp 在这个系统上读不到机身温度。
func DeviceTemp() (float64, bool) { return 0, false }
