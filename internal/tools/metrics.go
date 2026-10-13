package tools

import (
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"os"
	"runtime"
)

// SystemMetrics 记录系统纯只读硬件指标（第 11.4.1 节）。
type SystemMetrics struct {
	OS             string `json:"os"`
	NumCPU         int    `json:"num_cpu"`
	MemTotalBytes  int64  `json:"mem_total_bytes"`
	MemAvailBytes  int64  `json:"mem_avail_bytes"`
	DiskFreeBytes  uint64 `json:"disk_free_bytes"`
	DiskTotalBytes uint64 `json:"disk_total_bytes"`
}

// GetSystemMetrics 采集当前系统硬件只读指标，严禁任何修改或电源操作。
func GetSystemMetrics(rootPath string) *SystemMetrics {
	m := &SystemMetrics{
		OS:     runtime.GOOS,
		NumCPU: runtime.NumCPU(),
	}

	if stats, err := mem.VirtualMemory(); err == nil {
		m.MemTotalBytes = int64(stats.Total)
		m.MemAvailBytes = int64(stats.Available)
	}
	if rootPath == "" {
		rootPath, _ = os.Getwd()
	}
	if stats, err := disk.Usage(rootPath); err == nil {
		m.DiskFreeBytes = stats.Free
		m.DiskTotalBytes = stats.Total
	}
	return m
}
