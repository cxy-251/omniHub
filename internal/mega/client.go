// Package mega 通过 MEGAcmd（mega-exec）提供 MEGA 网盘：浏览、下载到本机目录、传输队列与配额倒计时。
// 遵循 ARCHITECTURE.md 第 10.2 节：只在使用时启动 mega-cmd-server，空闲且没有进行中的传输时关闭。
package mega

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable 表示本机没有安装 MEGAcmd。
var ErrUnavailable = errors.New("MEGAcmd 未安装")

// Client 调用 MEGAcmd 的命令行客户端。mega-exec 在服务未运行时会自己拉起 mega-cmd-server，
// 登录会话保存在 ~/.megaCmd，服务重启后自动恢复。
type Client struct {
	dir  string // MEGAcmd 安装目录（含 bin/、lib/）；空表示用 PATH 里的 mega-exec
	idle time.Duration

	cmdMu sync.Mutex // lcd + get 这类依赖服务端工作目录的命令串行执行

	mu         sync.Mutex
	lastUse    time.Time
	quotaUntil time.Time
	stop       chan struct{}
}

// NewClient 创建客户端并启动空闲检查；dir 为安装目录。
func NewClient(dir string, idle time.Duration) *Client {
	if idle <= 0 {
		idle = 5 * time.Minute
	}
	c := &Client{dir: dir, idle: idle, stop: make(chan struct{})}
	go c.idleLoop()
	return c
}

// Close 停止空闲检查，并在没有传输时关闭 MEGA 服务。
func (c *Client) Close() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	if c.serverRunning() && !c.busy() {
		c.quit()
	}
}

func (c *Client) bin(name string) string {
	if c.dir != "" {
		return filepath.Join(c.dir, "bin", name)
	}
	return name
}

// Available 判断 MEGAcmd 是否可用。
func (c *Client) Available() bool {
	if c.dir != "" {
		info, err := os.Stat(c.bin("mega-exec"))
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath("mega-exec")
	return err == nil
}

func (c *Client) env() []string {
	env := os.Environ()
	if c.dir == "" {
		return env
	}
	return append(env, "PATH="+filepath.Join(c.dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LD_LIBRARY_PATH="+filepath.Join(c.dir, "lib"))
}

// Result 是一条命令的输出。
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

func (r Result) message() string {
	if s := strings.TrimSpace(r.Stderr); s != "" {
		return s
	}
	return strings.TrimSpace(r.Stdout)
}

// run 执行 mega-exec 子命令。会话恢复期间命令会被拒绝（"while login in"），这时稍等重试。
func (c *Client) run(ctx context.Context, timeout time.Duration, args ...string) (Result, error) {
	if !c.Available() {
		return Result{}, ErrUnavailable
	}
	c.touch()
	var res Result
	for attempt := 0; attempt < 10; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		cmd := exec.CommandContext(cctx, c.bin("mega-exec"), args...)
		cmd.Env = c.env()
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		cancel()
		res = Result{Stdout: out.String(), Stderr: errOut.String()}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			res.Code = exit.ExitCode()
		} else if err != nil {
			return res, err
		}
		if !strings.Contains(res.Stderr, "while login in") {
			break
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return res, nil
}

func (c *Client) touch() {
	c.mu.Lock()
	c.lastUse = time.Now()
	c.mu.Unlock()
}

// serverRunning 看 MEGAcmd 的套接字是否存在（服务退出时会删掉它）。
func (c *Client) serverRunning() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".megaCmd", "megacmd.socket"))
	return err == nil
}

func (c *Client) quit() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin("mega-exec"), "quit")
	cmd.Env = c.env()
	_ = cmd.Run()
}

// busy 判断是否有进行中的传输（不更新最近使用时间）。
func (c *Client) busy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin("mega-exec"), "transfers", "--col-separator=|||", "--limit=50")
	cmd.Env = c.env()
	out, err := cmd.Output()
	if err != nil {
		return true // 拿不准时不关
	}
	return len(parseTransfers(string(out))) > 0
}

// idleLoop：页面不再请求超过 idle 且没有传输时关闭服务，释放内存。
func (c *Client) idleLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
		}
		c.mu.Lock()
		idle := !c.lastUse.IsZero() && time.Since(c.lastUse) > c.idle
		c.mu.Unlock()
		if idle && c.Available() && c.serverRunning() && !c.busy() {
			c.quit()
			c.mu.Lock()
			c.lastUse = time.Time{}
			c.mu.Unlock()
		}
	}
}

// Status 是账号与配额概况。
type Status struct {
	Available   bool      `json:"available"`
	LoggedIn    bool      `json:"logged_in"`
	Email       string    `json:"email,omitempty"`
	Used        string    `json:"used,omitempty"`
	Total       string    `json:"total,omitempty"`
	OverQuota   bool      `json:"over_quota"`
	QuotaUntil  time.Time `json:"quota_until,omitzero"`
	QuotaRemain int64     `json:"quota_remaining_sec,omitempty"`
	Error       string    `json:"error,omitempty"`
}

var (
	emailPattern   = regexp.MustCompile(`Account e-mail:\s*(\S+)`)
	storagePattern = regexp.MustCompile(`USED STORAGE:\s*([\d.]+\s*\S+)\s+[\d.]+% of\s+([\d.]+\s*\S+)`)
	retryPattern   = regexp.MustCompile(`(?i)try again in (\d+)`)
)

// GetStatus 查询登录账号、容量与配额。
func (c *Client) GetStatus(ctx context.Context) Status {
	st := Status{Available: c.Available()}
	if !st.Available {
		return st
	}
	res, err := c.run(ctx, 40*time.Second, "whoami")
	if err != nil {
		st.Error = err.Error()
		return st
	}
	if m := emailPattern.FindStringSubmatch(res.Stdout); m != nil {
		st.LoggedIn, st.Email = true, m[1]
		if df, err := c.run(ctx, 20*time.Second, "df", "-h"); err == nil {
			if m := storagePattern.FindStringSubmatch(df.Stdout); m != nil {
				st.Used, st.Total = m[1], m[2]
			}
		}
	}
	c.fillQuota(&st)
	return st
}

func (c *Client) fillQuota(st *Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if remain := time.Until(c.quotaUntil); remain > 0 {
		st.OverQuota, st.QuotaUntil, st.QuotaRemain = true, c.quotaUntil, int64(remain.Seconds())
	}
}

// noteQuota 从错误输出里识别带宽配额超限，记下解除时间（读不到等待时间时按 2 小时估计）。
func (c *Client) noteQuota(text string) bool {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "quota") && !strings.Contains(lower, "try again in") {
		return false
	}
	wait := 2 * time.Hour
	if m := retryPattern.FindStringSubmatch(text); m != nil {
		if s, err := strconv.Atoi(m[1]); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
	}
	c.mu.Lock()
	c.quotaUntil = time.Now().Add(wait)
	c.mu.Unlock()
	return true
}

// Login 登录账号；开了两步验证时需要 authCode。
func (c *Client) Login(ctx context.Context, email, password, authCode string) error {
	args := []string{"login"}
	if authCode != "" {
		args = append(args, "--auth-code="+authCode)
	}
	res, err := c.run(ctx, 90*time.Second, append(args, email, password)...)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		msg := res.message()
		lower := strings.ToLower(msg)
		switch {
		case strings.Contains(lower, "auth code") || strings.Contains(lower, "two-factor") || strings.Contains(lower, "mfa"):
			return errors.New("需要两步验证码")
		case strings.Contains(lower, "invalid email or password") || strings.Contains(lower, "incorrect"):
			return errors.New("邮箱或密码错误")
		}
		return fmt.Errorf("登录失败：%s", msg)
	}
	return nil
}

// Simple 执行无输出解析的命令（logout、reload 等），失败时返回命令输出。
func (c *Client) Simple(ctx context.Context, timeout time.Duration, args ...string) error {
	res, err := c.run(ctx, timeout, args...)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return errors.New(res.message())
	}
	return nil
}

// Entry 是云端目录里的一项。
type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	MTime string `json:"mtime"`
}

// List 列出云端目录（/ 为根目录，//bin 为回收站）。
func (c *Client) List(ctx context.Context, remote string) ([]Entry, error) {
	res, err := c.run(ctx, 40*time.Second, "ls", "-l", "--time-format=ISO6081_WITH_TIME", remote)
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, errors.New(res.message())
	}
	return parseList(res.Stdout, remote), nil
}

func parseList(out, remote string) []Entry {
	base := strings.TrimSuffix(remote, "/")
	list := []Entry{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "FLAGS") || strings.HasPrefix(line, "Total") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 5 {
			continue
		}
		// FLAGS VERS SIZE DATE NAME（名称可能含空格）
		name := strings.TrimSpace(line[strings.Index(line, parts[3])+len(parts[3]):])
		e := Entry{Name: name, Path: base + "/" + name, IsDir: strings.HasPrefix(parts[0], "d"), MTime: parts[3]}
		if n, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
			e.Size = n
		}
		list = append(list, e)
	}
	return list
}

// Download 把云端路径或公开链接下载到本机目录 dest（后台进行，用 Transfers 查看进度）。
func (c *Client) Download(ctx context.Context, source, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()
	args := []string{"get", "-q", "--ignore-quota-warn", source}
	if strings.HasPrefix(source, "http") || strings.HasSuffix(source, "/") {
		// 公开链接和目录：交给服务端当前工作目录，避免把目标目录当成重名文件（生成 "Downloads (1)"）。
		if err := c.Simple(ctx, 10*time.Second, "lcd", dest); err != nil {
			return err
		}
	} else {
		args = append(args, filepath.Join(dest, filepath.Base(source)))
	}
	res, err := c.run(ctx, 60*time.Second, args...)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		msg := res.message()
		if c.noteQuota(msg) {
			return fmt.Errorf("MEGA 带宽配额已用完：%s", msg)
		}
		return errors.New(msg)
	}
	return nil
}

// Transfer 是传输队列里的一项。
type Transfer struct {
	Type     string  `json:"type"`
	Tag      string  `json:"tag"`
	Name     string  `json:"name"`
	Source   string  `json:"source"`
	Dest     string  `json:"dest"`
	Progress string  `json:"progress"`
	Percent  float64 `json:"percent"`
	State    string  `json:"state"`
}

var percentPattern = regexp.MustCompile(`([\d.]+)%`)

func parseTransfers(out string) []Transfer {
	list := []Transfer{}
	for _, line := range strings.Split(out, "\n") {
		cols := strings.Split(strings.TrimSpace(line), "|||")
		if len(cols) < 6 || cols[1] == "TAG" {
			continue
		}
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		state := strings.ToUpper(cols[5])
		if strings.Contains(state, "CANCEL") || strings.Contains(state, "COMPLET") {
			continue
		}
		t := Transfer{Type: cols[0], Tag: cols[1], Source: cols[2], Dest: cols[3], Progress: cols[4], State: cols[5], Name: filepath.Base(cols[2])}
		if m := percentPattern.FindStringSubmatch(cols[4]); m != nil {
			t.Percent, _ = strconv.ParseFloat(m[1], 64)
		}
		list = append(list, t)
	}
	return list
}

// Transfers 返回进行中的传输。有卡在重试/阻塞状态的任务而还没记下配额时间时，按配额超限估计 2 小时。
func (c *Client) Transfers(ctx context.Context) ([]Transfer, Status, error) {
	var st Status
	res, err := c.run(ctx, 20*time.Second, "transfers", "--col-separator=|||", "--limit=50")
	if err != nil {
		return nil, st, err
	}
	list := parseTransfers(res.Stdout)
	c.mu.Lock()
	known := time.Until(c.quotaUntil) > 0
	c.mu.Unlock()
	if !known {
		for _, t := range list {
			s := strings.ToUpper(t.State)
			if strings.Contains(s, "RETRY") || strings.Contains(s, "BLOCK") {
				c.noteQuota("quota")
				break
			}
		}
	}
	c.fillQuota(&st)
	return list, st, nil
}

// ControlTransfer 暂停 / 继续 / 取消一个传输（tag 为 all 时作用于全部）。
func (c *Client) ControlTransfer(ctx context.Context, tag, action string) error {
	flag := map[string]string{"pause": "-p", "resume": "-r", "cancel": "-c"}[action]
	if flag == "" {
		return errors.New("unknown action")
	}
	target := tag
	if tag == "all" {
		target = "-a"
	}
	return c.Simple(ctx, 20*time.Second, "transfers", flag, target)
}
