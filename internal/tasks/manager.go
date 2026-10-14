package tasks

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"omnihub/internal/platform"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// LogLevel 是任务输出中识别的日志级别。
type LogLevel string

const (
	LevelDebug LogLevel = "DEBUG"
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
)

// LogEntry 是有限日志缓冲中的一条输出，ID 单调递增。
type LogEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	DeltaMs   int64     `json:"delta_ms"`
	Level     LogLevel  `json:"level"`
	Message   string    `json:"message"`
	IsStack   bool      `json:"is_stack"`
}

// Task 管理单个脚本进程；可变状态通过内部锁与 JSON 快照读取。
type Task struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Args      []string               `json:"args"`
	Running   bool                   `json:"running"`
	StartedAt time.Time              `json:"started_at"`
	EndedAt   *time.Time             `json:"ended_at,omitempty"`
	ExitCode  int                    `json:"exit_code"`
	cmd       *exec.Cmd              `json:"-"`
	group     *platform.ProcessGroup `json:"-"`
	logsMu    sync.RWMutex
	logs      []LogEntry
	nextLogID int64
	lastTime  time.Time
	listeners map[chan LogEntry]struct{}
	done      chan struct{}
	logNext   int
}

// Manager 保存有限任务历史并管理其生命周期。
type Manager struct {
	mu     sync.RWMutex
	tasks  map[string]*Task
	closed bool
}

// NewManager 创建有限容量的任务管理器。
func NewManager() *Manager {
	return &Manager{
		tasks: make(map[string]*Task),
	}
}

// HasRunningTasks 判断是否有正在写资源库的任务；配置切换必须先由用户停止它们。
func (m *Manager) HasRunningTasks() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, task := range m.tasks {
		task.logsMu.RLock()
		running := task.Running
		task.logsMu.RUnlock()
		if running {
			return true
		}
	}
	return false
}

// RunScript 运行已通过调用方注册和校验的脚本，相同 ID 同时仅有一个实例。
func (m *Manager) RunScript(id, scriptPath string, args []string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.New("task manager is closed")
	}
	if id == "" || scriptPath == "" {
		return nil, errors.New("task ID and script are required")
	}
	if t, exists := m.tasks[id]; exists && t.isRunning() {
		return nil, fmt.Errorf("task %s already running", id)
	}

	if len(m.tasks) >= MaxTasks {
		var oldest *Task
		for _, t := range m.tasks {
			if !t.isRunning() && (oldest == nil || t.StartedAt.Before(oldest.StartedAt)) {
				oldest = t
			}
		}
		if oldest == nil {
			return nil, errors.New("task capacity reached")
		}
		delete(m.tasks, oldest.ID)
	}
	cmd := platform.BackgroundCommand(context.Background(), scriptPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	t := &Task{
		ID:        id,
		Name:      scriptPath,
		Args:      append([]string(nil), args...),
		Running:   true,
		StartedAt: time.Now(),
		cmd:       cmd,
		logs:      make([]LogEntry, 0, MaxLogEntries),
		done:      make(chan struct{}),
		listeners: make(map[chan LogEntry]struct{}),
		lastTime:  time.Now(),
	}

	group, err := platform.StartGroup(cmd, false)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	t.group = group
	m.tasks[id] = t

	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), MaxLogLineBytes)
		for scanner.Scan() {
			line := scanner.Text()
			t.appendLog(line)
		}

		if err := scanner.Err(); err != nil {
			t.appendLog("ERROR: task output failed: " + err.Error())
			// 输出管道失效时阻止子进程无限写入未消费的管道。
			if killErr := t.group.Stop(true); killErr != nil {
				t.appendLog("ERROR: stop task: " + killErr.Error())
			}
		}
	}()
	go func() {
		if err := group.Wait(func() error {
			select {
			case <-outputDone:
				return nil
			case <-time.After(2 * time.Second):
				closeErr := stdout.Close()
				<-outputDone
				return errors.Join(errors.New("task output did not close after group cleanup"), closeErr)
			}
		}); err != nil {
			t.appendLog("ERROR: task exited: " + err.Error())
		}
		t.logsMu.Lock()
		t.Running = false
		now := time.Now()
		t.EndedAt = &now
		if cmd.ProcessState != nil {
			t.ExitCode = cmd.ProcessState.ExitCode()
		}
		close(t.done)
		// 关闭所有监听通道
		for ch := range t.listeners {
			close(ch)
			delete(t.listeners, ch)
		}
		t.logsMu.Unlock()
	}()

	return t, nil
}

func (t *Task) appendLog(msg string) {
	t.logsMu.Lock()
	defer t.logsMu.Unlock()

	if len(msg) > 4096 {
		msg = msg[:4096] + " … [truncated]"
	}
	now := time.Now()
	delta := now.Sub(t.lastTime).Milliseconds()
	t.lastTime = now

	t.nextLogID++
	entry := LogEntry{
		ID:        t.nextLogID,
		Timestamp: now,
		DeltaMs:   delta,
		Level:     detectLogLevel(msg),
		Message:   msg,
		IsStack:   detectIsStack(msg),
	}
	if len(t.logs) < MaxLogEntries {
		t.logs = append(t.logs, entry)
	} else {
		t.logs[t.logNext] = entry
		t.logNext = (t.logNext + 1) % MaxLogEntries
	}

	for ch := range t.listeners {
		select {
		case ch <- entry:
		default:
		}
	}
}

const (
	// MaxTasks 限制运行中与已完成任务总数，满时淘汰最早完成的历史任务。
	MaxTasks = 16
	// MaxLogEntries 限制每个任务保留的日志行数。
	MaxLogEntries = 256
	// MaxLogLineBytes 限制一行输出大小，避免异常输出耗尽内存。
	MaxLogLineBytes = 16 * 1024
)

var errorPattern = regexp.MustCompile(`(?i)error|fatal|exception`)
var warningPattern = regexp.MustCompile(`(?i)warn`)
var debugPattern = regexp.MustCompile(`(?i)debug`)
var stackPattern = regexp.MustCompile(`^\s+at\s+|^\s+File\s+".*",\s+line\s+\d+|panic:`)

func detectLogLevel(msg string) LogLevel {
	if errorPattern.MatchString(msg) {
		return LevelError
	}
	if warningPattern.MatchString(msg) {
		return LevelWarn
	}
	if debugPattern.MatchString(msg) {
		return LevelDebug
	}
	return LevelInfo
}
func detectIsStack(msg string) bool { return stackPattern.MatchString(msg) }

func (t *Task) isRunning() bool {
	t.logsMu.RLock()
	defer t.logsMu.RUnlock()
	return t.Running
}

// MarshalJSON 在锁内复制状态，避免 HTTP 编码与退出 goroutine 数据竞争。
func (t *Task) MarshalJSON() ([]byte, error) {
	t.logsMu.RLock()
	defer t.logsMu.RUnlock()
	return json.Marshal(struct {
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		Args      []string   `json:"args"`
		Running   bool       `json:"running"`
		StartedAt time.Time  `json:"started_at"`
		EndedAt   *time.Time `json:"ended_at,omitempty"`
		ExitCode  int        `json:"exit_code"`
	}{t.ID, t.Name, t.Args, t.Running, t.StartedAt, t.EndedAt, t.ExitCode})
}

// StopTask 请求任务进程组退出，五秒未完成才强杀；已完成的任务不再发送信号。
func (m *Manager) StopTask(id string) error {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok || !t.isRunning() {
		return fmt.Errorf("task %s not running", id)
	}

	if err := t.group.Stop(false); err != nil {
		return fmt.Errorf("stop task: %w", err)
	}
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-t.done:
			return
		case <-timer.C:
			if t.isRunning() {
				if err := t.group.Stop(true); err != nil {
					t.appendLog("ERROR: kill task: " + err.Error())
				}
			}
		}
	}()
	return nil
}

// Close 禁止启动新任务并终止当前任务，等待全部输出与进程回收。
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	current := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		current = append(current, t)
	}
	m.mu.Unlock()
	var errs []error
	for _, t := range current {
		if t.isRunning() {
			errs = append(errs, m.StopTask(t.ID))
		}
	}
	for _, t := range current {
		<-t.done
	}
	return errors.Join(errs...)
}

// GetTask 返回任务实例，不存在时返回 nil。

func (m *Manager) GetTask(id string) *Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tasks[id]
}

// SubscribeLogs 返回按 ID 排序的有限历史及后续输出；结束或取消时关闭通道。
func (t *Task) SubscribeLogs(ctx context.Context) (chan LogEntry, []LogEntry) {
	t.logsMu.Lock()
	ch := make(chan LogEntry, 100)
	history := make([]LogEntry, 0, len(t.logs))
	history = append(history, t.logs[t.logNext:]...)
	history = append(history, t.logs[:t.logNext]...)
	if !t.Running || len(t.listeners) >= 8 {
		close(ch)
		t.logsMu.Unlock()
		return ch, history
	}
	t.listeners[ch] = struct{}{}
	t.logsMu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
		case <-t.done:
		}
		t.logsMu.Lock()
		if _, exists := t.listeners[ch]; exists {
			delete(t.listeners, ch)
			close(ch)
		}
		t.logsMu.Unlock()
	}()
	return ch, history
}
