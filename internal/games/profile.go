package games

import "sync"

// Profile 是一次运行的硬件画像：取自游戏所在 systemd scope 的 cgroup，覆盖 Wine 等全部子进程。
type Profile struct {
	PeakMemMB  int64   `json:"peak_mem_mb"`  // 内存峰值（cgroup memory.stat 的 anon + shmem，每秒采样）
	PeakIOMBps float64 `json:"peak_io_mbps"` // 每秒读写合计的峰值
	AvgCPUPct  float64 `json:"avg_cpu_pct"`  // 平均 CPU，100 = 占满一个核
	Seconds    int64   `json:"seconds"`      // 采样覆盖的时长
}

// profileSampler 由看门狗每秒调用 sample；游戏退出后用 result 取画像。scope 被回收后读不到，保留最后一次的值。
type profileSampler struct {
	mu      sync.Mutex
	pid     int
	scope   string
	dir     string
	peak    Profile
	ioLast  uint64
	ioTotal uint64
	ioByPID map[string]uint64 // 各进程上次读到的读写字节
	ioAt    int64             // 上次 io 采样的毫秒时间戳
	cpu0    uint64
	cpuN    uint64
	t0, tN  int64
	ok      bool
}

func (s *profileSampler) result() *Profile {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ok {
		return nil
	}
	p := s.peak
	if span := s.tN - s.t0; span > 0 {
		p.Seconds = span / 1000
		p.AvgCPUPct = float64(s.cpuN-s.cpu0) / float64(span*1000) * 100
	}
	return &p
}
