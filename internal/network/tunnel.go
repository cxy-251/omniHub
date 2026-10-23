package network

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// cloudflareEdges 是 Cloudflare 隧道边缘节点。大陆网络下 argotunnel.com 的 SRV 解析被污染、
// TCP/HTTP2 握手被重置，只有写死边缘 IP + QUIC 能连上（omni-deck 2026-09 实测能注册满 4 条连接）。
var cloudflareEdges = []string{
	"198.41.192.7", "198.41.192.27", "198.41.192.37", "198.41.192.47",
	"198.41.192.67", "198.41.192.107", "198.41.192.167", "198.41.192.227",
	"198.41.200.13", "198.41.200.23", "198.41.200.33", "198.41.200.43",
	"198.41.200.53", "198.41.200.113", "198.41.200.193", "198.41.200.233",
}

// TunnelStatus 是隧道的当前状态，给设置页显示。
type TunnelStatus struct {
	Enabled     bool   `json:"enabled"`
	Running     bool   `json:"running"`
	Connections int    `json:"connections"` // 已注册到边缘的连接数，大于 0 才真正能从公网访问
	Domain      string `json:"domain"`      // 配置文件 ingress 里的第一个 hostname
	Service     string `json:"service"`     // 该 hostname 转发到的本机地址
	Binary      string `json:"binary"`
	Config      string `json:"config"`
	Error       string `json:"error,omitempty"`
}

// Tunnel 按开关托管一个 cloudflared 进程（命名隧道，用户自己的 config.yml）。
// 打开时启动并在退出后 3～30 秒退避重启；关闭或服务退出时结束进程。
type Tunnel struct {
	binary, config string
	mu             sync.Mutex
	cancel         context.CancelFunc
	done           chan struct{}
	status         TunnelStatus
}

// NewTunnel 记下 cloudflared 程序与配置文件位置；binary 为空表示没找到。
func NewTunnel(binary, config string) *Tunnel {
	t := &Tunnel{binary: binary, config: config}
	t.status.Binary, t.status.Config = binary, config
	t.status.Domain, t.status.Service = ingress(config)
	return t
}

// Status 返回当前状态的副本。
func (t *Tunnel) Status() TunnelStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.status
	s.Domain, s.Service = ingress(t.config) // 用户可能刚改过配置文件
	return s
}

// SetEnabled 打开或关闭隧道；关闭时等 cloudflared 退出后才返回。
func (t *Tunnel) SetEnabled(enabled bool) {
	t.mu.Lock()
	t.status.Enabled = enabled
	t.status.Error = ""
	if !enabled {
		cancel, done := t.cancel, t.done
		t.cancel, t.done = nil, nil
		t.mu.Unlock()
		if cancel != nil {
			cancel()
			<-done
		}
		return
	}
	defer t.mu.Unlock()
	if t.cancel != nil {
		return
	}
	if t.binary == "" {
		t.status.Error = "没有找到 cloudflared（放在 ~/Applications/cloudflared/cloudflared）"
		return
	}
	if _, err := os.Stat(t.config); err != nil {
		t.status.Error = "没有找到隧道配置 " + t.config
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel, t.done = cancel, make(chan struct{})
	go t.supervise(ctx, t.done)
}

func (t *Tunnel) supervise(ctx context.Context, done chan struct{}) {
	defer close(done)
	backoff := 3 * time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := t.runOnce(ctx)
		if ctx.Err() != nil {
			break
		}
		t.mu.Lock()
		if err != nil {
			t.status.Error = err.Error()
		}
		t.mu.Unlock()
		// 连上过一段时间再断的，从短间隔重来；一启动就失败的逐次加长。
		if time.Since(started) > time.Minute {
			backoff = 3 * time.Second
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	t.mu.Lock()
	t.status.Running, t.status.Connections = false, 0
	t.mu.Unlock()
}

// runOnce 运行一次 cloudflared，按它的日志统计已注册的连接数，直到进程退出。
func (t *Tunnel) runOnce(ctx context.Context) error {
	args := []string{"--config", t.config, "--protocol", "quic", "--no-autoupdate"}
	for _, ip := range cloudflareEdges {
		args = append(args, "--edge", ip+":7844")
	}
	cmd := exec.CommandContext(ctx, t.binary, append(args, "tunnel", "run")...)
	cmd.SysProcAttr = childProcAttr()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	t.mu.Lock()
	t.status.Running, t.status.Connections, t.status.Error = true, 0, ""
	t.mu.Unlock()
	scanner := bufio.NewScanner(stderr)
	var last string
	for scanner.Scan() {
		line := scanner.Text()
		t.mu.Lock()
		switch {
		case strings.Contains(line, "Registered tunnel connection"):
			t.status.Connections++
			t.status.Error = ""
		case strings.Contains(line, "Unregistered tunnel connection") || strings.Contains(line, "Connection terminated"):
			t.status.Connections = max(0, t.status.Connections-1)
		// 拿不到 cfd-features（大陆网络下这个域名解析不到）只是提示，隧道照常连接，不当成错误显示。
		case strings.Contains(line, " ERR ") && !strings.Contains(line, "Failed to fetch features"):
			last = strings.TrimSpace(line[strings.Index(line, " ERR ")+5:])
			t.status.Error = last
		}
		t.mu.Unlock()
	}
	err = cmd.Wait()
	t.mu.Lock()
	t.status.Running, t.status.Connections = false, 0
	t.mu.Unlock()
	if last != "" {
		return &tunnelError{last}
	}
	return err
}

type tunnelError struct{ msg string }

func (e *tunnelError) Error() string { return e.msg }

// ingress 从 cloudflared 的 config.yml 里读出第一条带 hostname 的规则（只做逐行查找，不解析整个 YAML）。
func ingress(config string) (hostname, service string) {
	data, err := os.ReadFile(config)
	if err != nil {
		return "", ""
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "-"))
		if v, ok := strings.CutPrefix(line, "hostname:"); ok && hostname == "" {
			hostname = strings.Trim(strings.TrimSpace(v), `"'`)
		} else if v, ok := strings.CutPrefix(line, "service:"); ok && hostname != "" && service == "" {
			service = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return hostname, service
}

// FindCloudflared 按「配置的路径 → ~/Applications/cloudflared/cloudflared → PATH」查找程序。
func FindCloudflared(configured string) string {
	candidates := []string{configured}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "Applications", "cloudflared", "cloudflared"))
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p
	}
	return ""
}

// DefaultTunnelConfig 是 cloudflared 默认的配置文件位置 ~/.cloudflared/config.yml。
func DefaultTunnelConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cloudflared", "config.yml")
}
