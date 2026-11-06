package tools

import (
	"container/heap"
	"context"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"

	"omnihub/internal/platform"
)

// SystemMetrics 记录系统纯只读硬件指标（第 11.4.1 节）。
type SystemMetrics struct {
	OS             string  `json:"os"`
	NumCPU         int     `json:"num_cpu"`
	CPUPercent     float64 `json:"cpu_percent"`
	TempCelsius    float64 `json:"temp_celsius,omitempty"`
	MemTotalBytes  int64   `json:"mem_total_bytes"`
	MemAvailBytes  int64   `json:"mem_avail_bytes"`
	DiskFreeBytes  uint64  `json:"disk_free_bytes"`
	DiskTotalBytes uint64  `json:"disk_total_bytes"`
	GoGoroutines   int     `json:"go_goroutines"`
	GoHeapBytes    uint64  `json:"go_heap_bytes"`
}

func readHwmonTemp() float64 {
	if t, ok := platform.DeviceTemp(); ok && t > 0 {
		return math.Round(t*10) / 10
	}
	inputs, _ := filepath.Glob("/sys/class/hwmon/hwmon*/temp*_input")
	for _, p := range inputs {
		if raw, err := os.ReadFile(p); err == nil {
			if v, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && v > 1000 && v < 120000 {
				return math.Round(float64(v)/100) / 10
			}
		}
	}
	return 0
}

// LargeFile 描述资源库中占用空间较大的文件条目。
type LargeFile struct {
	Library   string `json:"library"`
	RelPath   string `json:"rel_path"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
}

type fileMinHeap []LargeFile

func (h fileMinHeap) Len() int           { return len(h) }
func (h fileMinHeap) Less(i, j int) bool { return h[i].SizeBytes < h[j].SizeBytes }
func (h fileMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fileMinHeap) Push(x any)        { *h = append(*h, x.(LargeFile)) }
func (h *fileMinHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// GetSystemMetrics 采集当前系统硬件只读指标，严禁任何修改或电源操作。
func GetSystemMetrics(rootPath string) *SystemMetrics {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m := &SystemMetrics{
		OS:           runtime.GOOS,
		NumCPU:       runtime.NumCPU(),
		TempCelsius:  readHwmonTemp(),
		GoGoroutines: runtime.NumGoroutine(),
		GoHeapBytes:  ms.HeapAlloc,
	}
	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		m.CPUPercent = math.Round(pcts[0]*10) / 10
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

// ScanTopFiles 枚举给定根目录（库名 -> 物理路径）中体积最大的前 limit 个普通文件。
func ScanTopFiles(ctx context.Context, roots []struct{ Name, Path string }, limit int) ([]LargeFile, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	h := &fileMinHeap{}
	heap.Init(h)
	for _, r := range roots {
		cleanRoot := filepath.Clean(r.Path)
		err := filepath.WalkDir(cleanRoot, func(p string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if name == ".git" || name == "node_modules" || name == ".venv" || name == "__pycache__" || name == "compatdata" || name == "shadercache" || strings.HasPrefix(name, ".Trash") {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() < 1<<20 {
				return nil
			}
			if h.Len() == limit && info.Size() <= (*h)[0].SizeBytes {
				return nil
			}
			rel, relErr := filepath.Rel(cleanRoot, p)
			if relErr != nil {
				rel = name
			}
			item := LargeFile{
				Library:   r.Name,
				RelPath:   filepath.ToSlash(rel),
				SizeBytes: info.Size(),
				ModTime:   info.ModTime().Format("2006-01-02 15:04"),
			}
			if h.Len() < limit {
				heap.Push(h, item)
			} else {
				(*h)[0] = item
				heap.Fix(h, 0)
			}
			return nil
		})
		if err != nil && ctx.Err() != nil {
			break
		}
	}
	out := make([]LargeFile, h.Len())
	copy(out, *h)
	sort.Slice(out, func(i, j int) bool {
		if out[i].SizeBytes != out[j].SizeBytes {
			return out[i].SizeBytes > out[j].SizeBytes
		}
		return out[i].RelPath < out[j].RelPath
	})
	return out, ctx.Err()
}
