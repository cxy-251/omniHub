package tasks

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sync"
	"syscall"
	"time"
)

type LogLevel string

const (
	LevelDebug LogLevel = "DEBUG"
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
)

type LogEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	DeltaMs   int64     `json:"delta_ms"`
	Level     LogLevel  `json:"level"`
	Message   string    `json:"message"`
	IsStack   bool      `json:"is_stack"`
}

type Task struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Args      []string    `json:"args"`
	Running   bool        `json:"running"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   *time.Time  `json:"ended_at,omitempty"`
	ExitCode  int         `json:"exit_code"`
	cmd       *exec.Cmd   `json:"-"`
	pgid      int         `json:"-"`
	logsMu    sync.RWMutex
	logs      []LogEntry
	nextLogID int64
	lastTime  time.Time
	listeners map[chan LogEntry]struct{}
}

type Manager struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

func NewManager() *Manager {
	return &Manager{
		tasks: make(map[string]*Task),
	}
}

func (m *Manager) RunScript(id, scriptPath string, args []string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, exists := m.tasks[id]; exists && t.Running {
		return nil, fmt.Errorf("task %s already running", id)
	}

	cmdArgs := []string{"-n", "10", "ionice", "-c", "3", scriptPath}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("nice", cmdArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	t := &Task{
		ID:        id,
		Name:      scriptPath,
		Args:      args,
		Running:   true,
		StartedAt: time.Now(),
		cmd:       cmd,
		logs:      make([]LogEntry, 0, 1000),
		listeners: make(map[chan LogEntry]struct{}),
		lastTime:  time.Now(),
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t.pgid = cmd.Process.Pid
	m.tasks[id] = t

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			t.appendLog(line)
		}

		_ = cmd.Wait()
		t.logsMu.Lock()
		t.Running = false
		now := time.Now()
		t.EndedAt = &now
		if cmd.ProcessState != nil {
			t.ExitCode = cmd.ProcessState.ExitCode()
		}
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
	t.logs = append(t.logs, entry)

	for ch := range t.listeners {
		select {
		case ch <- entry:
		default:
		}
	}
}

func detectLogLevel(msg string) LogLevel {
	matched, _ := regexp.MatchString(`(?i)error|fatal|exception`, msg)
	if matched {
		return LevelError
	}
	matched, _ = regexp.MatchString(`(?i)warn`, msg)
	if matched {
		return LevelWarn
	}
	matched, _ = regexp.MatchString(`(?i)debug`, msg)
	if matched {
		return LevelDebug
	}
	return LevelInfo
}

func detectIsStack(msg string) bool {
	matched, _ := regexp.MatchString(`^\s+at\s+|^\s+File\s+".*",\s+line\s+\d+|panic:`, msg)
	return matched
}

func (m *Manager) StopTask(id string) error {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok || !t.Running {
		return fmt.Errorf("task %s not running", id)
	}

	// 向进程组广播 SIGTERM
	if t.pgid > 0 {
		_ = syscall.Kill(-t.pgid, syscall.SIGTERM)
	}

	// 5秒后如果未退出则 SIGKILL
	go func(pgid int) {
		time.Sleep(5 * time.Second)
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}(t.pgid)

	return nil
}

func (m *Manager) GetTask(id string) *Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tasks[id]
}

func (t *Task) SubscribeLogs(ctx context.Context) (chan LogEntry, []LogEntry) {
	t.logsMu.Lock()
	ch := make(chan LogEntry, 100)
	t.listeners[ch] = struct{}{}
	history := make([]LogEntry, len(t.logs))
	copy(history, t.logs)
	t.logsMu.Unlock()

	go func() {
		<-ctx.Done()
		t.logsMu.Lock()
		delete(t.listeners, ch)
		t.logsMu.Unlock()
	}()

	return ch, history
}
