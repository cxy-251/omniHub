package tools

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// SystemMetrics 记录系统纯只读硬件指标（第 11.4.1 节）。
type SystemMetrics struct {
	OS            string  `json:"os"`
	NumCPU        int     `json:"num_cpu"`
	MemTotalBytes int64   `json:"mem_total_bytes"`
	MemAvailBytes int64   `json:"mem_avail_bytes"`
	DiskFreeBytes uint64  `json:"disk_free_bytes"`
	DiskTotalBytes uint64 `json:"disk_total_bytes"`
}

// GetSystemMetrics 采集当前系统硬件只读指标，严禁任何修改或电源操作。
func GetSystemMetrics(rootPath string) *SystemMetrics {
	m := &SystemMetrics{
		OS:     runtime.GOOS,
		NumCPU: runtime.NumCPU(),
	}

	// 1. 读取内存 (/proc/meminfo)
	if file, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if fields[0] == "MemTotal:" {
					kb, _ := strconv.ParseInt(fields[1], 10, 64)
					m.MemTotalBytes = kb * 1024
				} else if fields[0] == "MemAvailable:" {
					kb, _ := strconv.ParseInt(fields[1], 10, 64)
					m.MemAvailBytes = kb * 1024
				}
			}
		}
		_ = file.Close()
	}

	// 2. 读取磁盘空间 (Statfs)
	if rootPath == "" {
		rootPath = "/"
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(rootPath, &stat); err == nil {
		m.DiskFreeBytes = stat.Bavail * uint64(stat.Bsize)
		m.DiskTotalBytes = stat.Blocks * uint64(stat.Bsize)
	}

	return m
}
