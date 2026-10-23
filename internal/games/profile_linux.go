package games

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// newProfileSampler 为游戏建采样器。只在游戏进了自己的 scope 时采样，否则 cgroup 是 omniHub 自己的，数字没有意义。
// systemd-run 先登记 scope 再 exec，刚启动时进程可能还不在里面，所以 cgroup 目录在采样时才找。
func newProfileSampler(pid int, scope string) *profileSampler {
	if scope == "" {
		return nil
	}
	return &profileSampler{pid: pid, scope: scope}
}

func (s *profileSampler) locate() bool {
	if s.dir != "" {
		return true
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(s.pid) + "/cgroup")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if rel, ok := strings.CutPrefix(line, "0::"); ok && strings.HasSuffix(rel, "/"+s.scope) {
			s.dir = filepath.Join("/sys/fs/cgroup", rel)
			return true
		}
	}
	return false
}

func (s *profileSampler) sample() {
	if s == nil || !s.locate() {
		return
	}
	now := time.Now().UnixMilli()
	// 内存只算 anon + shmem（游戏自己占的，Wine 的共享内存在 shmem 里）；memory.peak 含读写文件的页缓存，会虚高。
	anon, memOK := readCgroupKey(filepath.Join(s.dir, "memory.stat"), "anon")
	shmem, _ := readCgroupKey(filepath.Join(s.dir, "memory.stat"), "shmem")
	mem := anon + shmem
	cpu, cpuOK := readCgroupKey(filepath.Join(s.dir, "cpu.stat"), "usage_usec")
	s.mu.Lock()
	defer s.mu.Unlock()
	io, ioOK := s.readIO()
	if memOK {
		s.peak.PeakMemMB = max(s.peak.PeakMemMB, int64(mem>>20))
		s.ok = true
	}
	if cpuOK {
		if s.t0 == 0 {
			s.cpu0, s.t0 = cpu, now
		}
		s.cpuN, s.tN = cpu, now
	}
	if ioOK {
		if s.ioAt > 0 && now > s.ioAt && io >= s.ioLast {
			rate := float64(io-s.ioLast) / float64(1<<20) / (float64(now-s.ioAt) / 1000)
			s.peak.PeakIOMBps = max(s.peak.PeakIOMBps, rate)
		}
		s.ioLast, s.ioAt = io, now
	}
}

func readCgroupKey(path, key string) (uint64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if v, ok := strings.CutPrefix(scanner.Text(), key+" "); ok {
			n, err := strconv.ParseUint(v, 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// readIO 返回 scope 里各进程磁盘读写字节的累计增量。SteamOS 的用户 systemd 没有下放 io 控制器（没有 io.stat），
// 所以按 cgroup.procs 逐个读 /proc/<pid>/io 的 read_bytes + write_bytes，累加每个进程自己的增量，进程退出不会让总数倒退。
// 两次采样之间起止的短命进程漏计（推测影响很小：游戏的大读写来自常驻进程）。
func (s *profileSampler) readIO() (uint64, bool) {
	data, err := os.ReadFile(filepath.Join(s.dir, "cgroup.procs"))
	if err != nil {
		return 0, false
	}
	seen := make(map[string]uint64)
	for _, pid := range strings.Fields(string(data)) {
		bytes, ok := readProcIO(pid)
		if !ok {
			continue
		}
		// 进程的计数从它 fork 时归零，scope 里的进程都是游戏起的，第一次见到时整个计数都算。
		if last, ok := s.ioByPID[pid]; !ok {
			s.ioTotal += bytes
		} else if bytes >= last {
			s.ioTotal += bytes - last
		}
		seen[pid] = bytes
	}
	s.ioByPID = seen
	return s.ioTotal, true
}

func readProcIO(pid string) (uint64, bool) {
	f, err := os.Open("/proc/" + pid + "/io")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var total uint64
	found := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ": ")
		if ok && (key == "read_bytes" || key == "write_bytes") {
			n, err := strconv.ParseUint(value, 10, 64)
			if err == nil {
				total += n
				found = true
			}
		}
	}
	return total, found
}
