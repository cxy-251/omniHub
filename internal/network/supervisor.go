package network

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// FindAvailablePort 从指定起始端口开始探测可用端口（第 8.2.3 节）。
func FindAvailablePort(startPort, maxAttempts int) (int, error) {
	for port := startPort; port < startPort+maxAttempts; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			_ = ln.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port found in range %d - %d", startPort, startPort+maxAttempts)
}

// TunnelState 记录当前隧道运行状态与捕获的公网域名。
type TunnelState struct {
	IsRunning bool      `json:"is_running"`
	PublicURL string    `json:"public_url"`
	LastError string    `json:"last_error"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CloudflareSupervisor 托管 cloudflared 进程生命周期（第 8.3 节）。
type CloudflareSupervisor struct {
	mu         sync.RWMutex
	binaryPath string
	targetPort int
	tunnelToken string
	state      TunnelState
}

// NewCloudflareSupervisor 创建隧道守护器实例。
func NewCloudflareSupervisor(binaryPath string, targetPort int, tunnelToken string) *CloudflareSupervisor {
	return &CloudflareSupervisor{
		binaryPath:  binaryPath,
		targetPort:  targetPort,
		tunnelToken: tunnelToken,
	}
}

// GetState 获取当前隧道状态。
func (s *CloudflareSupervisor) GetState() TunnelState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Run 启动隧道守护循环，支持输出正则提取公开域名与指数退避重连（第 8.3.1 节）。
func (s *CloudflareSupervisor) Run(ctx context.Context) {
	domainRegex := regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)
	backoff := 1 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.mu.Lock()
		s.state.IsRunning = true
		s.state.UpdatedAt = time.Now()
		s.mu.Unlock()

		var args []string
		if s.tunnelToken != "" {
			args = []string{"tunnel", "run", "--token", s.tunnelToken}
		} else {
			// 临时试用模式
			args = []string{"tunnel", "--url", fmt.Sprintf("http://localhost:%d", s.targetPort)}
		}

		cmd := exec.CommandContext(ctx, s.binaryPath, args...)
		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			s.recordError(err.Error())
			time.Sleep(backoff)
			continue
		}

		if err := cmd.Start(); err != nil {
			s.recordError(err.Error())
			time.Sleep(backoff)
			backoff = min(backoff*2, 30*time.Second)
			continue
		}

		// 异步监听输出提取公开域名
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				line := scanner.Text()
				match := domainRegex.FindString(line)
				if match != "" {
					s.mu.Lock()
					s.state.PublicURL = match
					s.state.UpdatedAt = time.Now()
					s.mu.Unlock()
				}
			}
		}()

		_ = cmd.Wait()

		s.mu.Lock()
		s.state.IsRunning = false
		s.state.PublicURL = ""
		s.state.UpdatedAt = time.Now()
		s.mu.Unlock()

		time.Sleep(backoff)
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (s *CloudflareSupervisor) recordError(errStr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.IsRunning = false
	s.state.LastError = errStr
	s.state.UpdatedAt = time.Now()
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
