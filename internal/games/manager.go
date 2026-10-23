package games

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"
)

// ErrRunBusy rejects a second native game while one is starting/running.
var ErrRunBusy = errors.New("a native game is already running")

// ErrRunClosed rejects starts after the owning service generation shuts down.
var ErrRunClosed = errors.New("game runtime manager closed")

// ErrRunUnknown distinguishes an absent/evicted session from a completed one.
var ErrRunUnknown = errors.New("game session not found")

// RunStatus is an immutable snapshot. A created process is running until its
// actual completion is observed; ExitError includes process/log cleanup failures.
type RunStatus struct {
	ID               string    `json:"id"`
	Key              string    `json:"key"`
	UserID           int64     `json:"user_id"`
	PID              int       `json:"pid"`
	State            string    `json:"state"`
	StartedAt        time.Time `json:"started_at"`
	EndedAt          time.Time `json:"ended_at,omitzero"`
	LogPath          string    `json:"log_path"`
	ExitError        string    `json:"exit_error,omitempty"`
	Warning          string    `json:"warning,omitempty"`
	PersistenceError string    `json:"persistence_error,omitempty"`
	Profile          *Profile  `json:"profile,omitempty"` // 退出后才有
}

type runSession struct {
	status   RunStatus
	game     *LaunchedGame
	done     chan struct{}
	cancel   context.CancelFunc
	sampler  *profileSampler
	stopOnce sync.Once
	stopErr  error
}

// Manager owns one active native game and at most 32 session snapshots. It has
// no idle timers or polling writes; the recorder runs only on completion.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*runSession
	active   *runSession
	closed   bool
	launch   func(context.Context, LaunchOptions) (*LaunchedGame, error)
	record   func(RunStatus) error
}

// NewManager creates an empty service-generation runtime manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*runSession), launch: LaunchGame}
}

// NewManagerWithRecorder runs record once after actual process/output cleanup,
// before publishing completion. The recorder must bound its own I/O duration;
// failures remain visible separately from the process exit error.
func NewManagerWithRecorder(record func(RunStatus) error) *Manager {
	m := NewManager()
	m.record = record
	return m
}

// Start creates a session for a caller-resolved library/game key. Request
// cancellation governs startup only, and no executable path is accepted by a
// future protocol layer without resolving its index record first.
func (m *Manager) Start(ctx context.Context, key string, opt LaunchOptions) (RunStatus, error) {
	if key == "" || len(key) > 4096 || opt.UserID <= 0 {
		return RunStatus{}, errors.New("invalid game session identity")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return RunStatus{}, ErrRunClosed
	}
	if m.active != nil {
		return RunStatus{}, ErrRunBusy
	}
	game, err := m.launch(ctx, opt)
	if err != nil {
		return RunStatus{}, err
	}
	for len(m.sessions) >= 32 {
		var oldest *runSession
		for _, entry := range m.sessions {
			if entry != m.active && (oldest == nil || entry.status.StartedAt.Before(oldest.status.StartedAt)) {
				oldest = entry
			}
		}
		delete(m.sessions, oldest.status.ID)
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	session := &runSession{status: RunStatus{ID: rand.Text(), Key: key, UserID: opt.UserID, PID: game.PID, State: "running", StartedAt: game.StartTime, LogPath: game.LogPath}, game: game, done: make(chan struct{}), cancel: cancel,
		sampler: newProfileSampler(game.PID, game.scope)}
	m.sessions[session.status.ID] = session
	m.active = session
	if err := StartWatchdog(watchCtx, game, time.Second, WatchdogCallbacks{
		OnWarning: func(int64) { m.warning(session, "系统可用内存不足，请保存游戏进度") },
		OnKilled:  func(int64) { m.warning(session, "系统内存临界，已请求强制停止游戏") },
		OnError:   func(err error) { m.warning(session, "内存看门狗不可用："+err.Error()) },
		OnTick:    session.sampler.sample,
	}); err != nil {
		session.status.Warning = err.Error()
	}
	go m.complete(session)
	return session.status, nil
}

func (m *Manager) warning(session *runSession, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == session {
		session.status.Warning = message
	}
}

func (m *Manager) complete(session *runSession) {
	err := session.game.Wait(context.Background())
	session.cancel()
	m.mu.Lock()
	completed := session.status
	m.mu.Unlock()
	completed.State = "exited"
	completed.EndedAt = time.Now()
	completed.Profile = session.sampler.result()
	if err != nil {
		completed.ExitError = err.Error()
	}
	// Keep admission occupied until persistence completes, so state migration and
	// service shutdown cannot close the destination beneath the recorder.
	if m.record != nil {
		if err := m.record(completed); err != nil {
			completed.PersistenceError = err.Error()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session.status = completed
	if m.active == session {
		m.active = nil
	}
	close(session.done)
}

// HasRunning reports whether a game still owns resources, including stopping.
func (m *Manager) HasRunning() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.active != nil }

// Active returns the current session copy, or nil when no game owns resources.
func (m *Manager) Active() *RunStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil
	}
	status := m.active.status
	return &status
}

// Snapshot returns a value copy without starting work or writing an index.
func (m *Manager) Snapshot(id string) (RunStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[id]
	if session == nil {
		return RunStatus{}, ErrRunUnknown
	}
	return session.status, nil
}

// Wait observes completion without cancelling the game when the caller leaves.
func (m *Manager) Wait(ctx context.Context, id string) (RunStatus, error) {
	m.mu.Lock()
	session := m.sessions[id]
	m.mu.Unlock()
	if session == nil {
		return RunStatus{}, ErrRunUnknown
	}
	select {
	case <-session.done:
		m.mu.Lock()
		defer m.mu.Unlock()
		return session.status, nil
	case <-ctx.Done():
		return RunStatus{}, ctx.Err()
	}
}

// Stop requests a bounded graceful stop once. It is harmless for completed
// sessions, and uses retained process ownership through timeout escalation.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	session := m.sessions[id]
	if session == nil {
		m.mu.Unlock()
		return ErrRunUnknown
	}
	if session.status.State == "exited" {
		m.mu.Unlock()
		return nil
	}
	session.status.State = "stopping"
	m.mu.Unlock()
	session.stopOnce.Do(func() {
		session.stopErr = session.game.Stop(false)
		go func() {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-session.done:
				return
			case <-timer.C:
				if err := session.game.Stop(true); err != nil {
					m.warning(session, "停止失败："+err.Error())
				}
			}
		}()
	})
	return session.stopErr
}

// Close rejects new starts, stops the active game and drains owned resources.
// Completed snapshots remain readable until the owning router is discarded.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	session := m.active
	m.mu.Unlock()
	if session == nil {
		return nil
	}
	session.cancel()
	stopErr := m.Stop(session.status.ID)
	timer := time.NewTimer(7 * time.Second)
	defer timer.Stop()
	select {
	case <-session.done:
		return stopErr
	case <-timer.C:
		return errors.Join(stopErr, errors.New("game shutdown did not drain"))
	}
}
