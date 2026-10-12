// Package mega 实现 MEGA 客户端套接字直连、按需唤醒与配额超限自动排队。
// 遵循 ARCHITECTURE.md 第 10.2 节规范。
package mega

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// QuotaState 记录 MEGA 带宽配额与倒计时（第 10.2.4 节）。
type QuotaState struct {
	IsOverQuota   bool      `json:"is_over_quota"`
	ResetTime     time.Time `json:"reset_time"`
	RemainingSec  int64     `json:"remaining_sec"`
	WaitDesc      string    `json:"wait_desc"`
}

// DownloadTask 记录 MEGA 下载任务状态。
type DownloadTask struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	DestDir   string    `json:"dest_dir"`
	Status    string    `json:"status"` // "queued", "downloading", "waiting_quota", "completed", "failed"
	Progress  float64   `json:"progress"`
	CreatedAt time.Time `json:"created_at"`
}

// Client 封装 MEGA-CMD 套接字直连与按需生命周期管理器。
type Client struct {
	mu           sync.Mutex
	socketPath   string
	serverCmd    *exec.Cmd
	lastActive   time.Time
	quotaState   QuotaState
	tasks        map[string]*DownloadTask
	idleTimeout  time.Duration
	stopIdleOnce sync.Once
}

// NewClient 初始化 MEGA 客户端。
func NewClient(socketPath string, idleTimeout time.Duration) *Client {
	if socketPath == "" {
		home, _ := os.UserHomeDir()
		socketPath = filepath.Join(home, ".megaCmd", "megacmd.socket")
	}
	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute // 默认 5 分钟空闲释放（第 10.2.2 节）
	}
	return &Client{
		socketPath:  socketPath,
		lastActive:  time.Now(),
		tasks:       make(map[string]*DownloadTask),
		idleTimeout: idleTimeout,
	}
}

// EnsureServerRunning 确保后台守护服务按需唤醒。
func (c *Client) EnsureServerRunning(serverBinPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastActive = time.Now()

	// 探测套接字是否可用
	if conn, err := net.Dial("unix", c.socketPath); err == nil {
		_ = conn.Close()
		return nil
	}

	if serverBinPath == "" {
		serverBinPath = "mega-cmd-server"
	}

	cmd := exec.Command(serverBinPath, "--do-not-log-to-stdout")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mega-cmd-server: %w", err)
	}
	c.serverCmd = cmd

	// 等待套接字生成
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if conn, err := net.Dial("unix", c.socketPath); err == nil {
			_ = conn.Close()
			return nil
		}
	}

	return fmt.Errorf("timeout waiting for megacmd.socket")
}

// CheckIdleAndShutdown 检查空闲超时并在无任务时主动退出守护进程，释放内存（第 10.2.2 节）。
func (c *Client) CheckIdleAndShutdown() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	hasActiveTasks := false
	for _, t := range c.tasks {
		if t.Status == "downloading" || t.Status == "queued" {
			hasActiveTasks = true
			break
		}
	}

	if !hasActiveTasks && time.Since(c.lastActive) > c.idleTimeout {
		if c.serverCmd != nil && c.serverCmd.Process != nil {
			_ = c.serverCmd.Process.Kill()
			c.serverCmd = nil
			return true
		}
	}
	return false
}

// EnqueueDownload 添加下载任务至指定目标目录，直存磁盘（第 10.2.3 节）。
func (c *Client) EnqueueDownload(id, source, destDir string) (*DownloadTask, error) {
	if source == "" || destDir == "" {
		return nil, errors.New("source and destDir cannot be empty")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastActive = time.Now()

	status := "downloading"
	if c.quotaState.IsOverQuota {
		status = "waiting_quota" // 配额超限自动排队挂起（第 10.2.4 节）
	}

	task := &DownloadTask{
		ID:        id,
		Source:    source,
		DestDir:   destDir,
		Status:    status,
		CreatedAt: time.Now(),
	}
	c.tasks[id] = task
	return task, nil
}

// HandleQuotaExceeded 捕获配额超限信号并自动挂起任务（第 10.2.4 节）。
func (c *Client) HandleQuotaExceeded(waitDuration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	resetTime := time.Now().Add(waitDuration)
	c.quotaState = QuotaState{
		IsOverQuota:  true,
		ResetTime:    resetTime,
		RemainingSec: int64(waitDuration.Seconds()),
		WaitDesc:     fmt.Sprintf("%d minutes", int(waitDuration.Minutes())),
	}

	// 挂起所有下载中的任务
	for _, t := range c.tasks {
		if t.Status == "downloading" {
			t.Status = "waiting_quota"
		}
	}
}

// CheckQuotaRecovery 检查配额是否恢复并唤醒挂起任务。
func (c *Client) CheckQuotaRecovery() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.quotaState.IsOverQuota {
		return true
	}

	if time.Now().After(c.quotaState.ResetTime) {
		c.quotaState.IsOverQuota = false
		c.quotaState.RemainingSec = 0
		c.quotaState.WaitDesc = ""

		// 恢复断点续传
		for _, t := range c.tasks {
			if t.Status == "waiting_quota" {
				t.Status = "downloading"
			}
		}
		return true
	}
	return false
}
