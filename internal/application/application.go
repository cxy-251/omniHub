// Package application 管理 HTTP 服务运行代际、资源库配置及全局状态恢复。
package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	mangajob "omnihub/features/manga"
	"omnihub/internal/auth"
	"omnihub/internal/config"
	"omnihub/internal/library"
	"omnihub/internal/server"
	"omnihub/internal/state"
	"omnihub/internal/tasks"
	"omnihub/web"
)

type generation struct {
	router     *server.Router
	authDB     *auth.DB
	libraries  *library.Manager
	ctx        context.Context
	cancel     context.CancelFunc
	stateError string
}

// Application 在端口保持绑定时切换数据库与路由，离线时保留恢复入口。
// operation 串行化配置事务，mu 只保护短暂的请求计数与运行代际交换。
type Application struct {
	operation    sync.Mutex
	mu           sync.Mutex
	cfg          *config.Config
	configDir    string
	localToken   string
	current      *generation
	active       int
	paused       bool
	drained      chan struct{}
	closed       bool
	confirmation *confirmation
	retryAt      time.Time
}

// New 创建服务生命周期控制器。全局状态暂不可用时返回离线实例，不创建替代账号。
func New(cfg *config.Config, configDir, localToken string) (*Application, error) {
	if cfg == nil || localToken == "" || len(localToken) > 256 || !filepath.IsAbs(configDir) {
		return nil, errors.New("configuration and local credential are required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ownedConfig, err := cloneConfig(cfg)
	if err != nil {
		return nil, err
	}
	a := &Application{cfg: ownedConfig, configDir: configDir, localToken: localToken}
	if err := state.Recover(a.journalPath(), cfg.StateDirectory); err != nil {
		log.Printf("Migration requires recovery: %v", err)
	}
	g := a.build()
	a.current = g
	return a, nil
}

func (a *Application) journalPath() string { return filepath.Join(a.configDir, "state-migration.json") }

func (a *Application) build() *generation {
	ctx, cancel := context.WithCancel(context.Background())
	g := &generation{ctx: ctx, cancel: cancel, libraries: library.NewManager()}
	configured, err := library.NewConfiguredManager(a.cfg.CategoryDirectories)
	if err != nil {
		g.stateError = err.Error()
		return g
	}
	g.libraries = configured
	for _, dir := range a.cfg.DisabledCategories {
		g.libraries.SetCategoryDisabled(dir, true)
	}
	if err := state.Recover(a.journalPath(), a.cfg.StateDirectory); err != nil {
		g.stateError = err.Error()
		return g
	}
	changed := false
	primaryID := ""
	paths := append([]string{}, a.cfg.Libraries...)
	if a.cfg.PrimaryLibrary != "" {
		paths = append([]string{a.cfg.PrimaryLibrary}, paths...)
	}
	seen := make(map[string]bool)
	for _, root := range paths {
		path, err := filepath.Abs(root)
		if err != nil {
			g.stateError = err.Error()
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		lib, err := g.libraries.MountKnownLibrary(path, false, a.cfg.IsLibraryReadOnly(path), a.cfg.KnownLibrary(path))
		if err != nil {
			log.Printf("Library mount %s: %v", path, err)
			continue
		}
		changed = a.cfg.RememberLibrary(lib) || changed
		primary, err := filepath.Abs(a.cfg.PrimaryLibrary)
		if err == nil && a.cfg.PrimaryLibrary != "" && primary == path {
			primaryID = lib.ID
		}
	}
	if err := g.libraries.SetPrimary(primaryID); err != nil {
		g.stateError = err.Error()
		return g
	}
	stateDir, stateChanged, err := a.cfg.ResolveStateDirectory(a.configDir, g.libraries.GetPrimary())
	if err != nil {
		g.stateError = err.Error()
		return g
	}
	if a.cfg.StateLibraryID == "" {
		for root, marker := range a.cfg.LibraryIdentities {
			if filepath.Join(root, library.DataDirName) == stateDir {
				a.cfg.StateLibraryRoot, a.cfg.StateLibraryID = root, marker.ID
				stateChanged = true
				if _, _, err := a.cfg.ResolveStateDirectory(a.configDir, g.libraries.GetPrimary()); err != nil {
					g.stateError = err.Error()
					return g
				}
				break
			}
		}
	}
	if changed || stateChanged {
		if err := config.Save(a.cfg); err != nil {
			g.stateError = err.Error()
			return g
		}
	}
	db, err := auth.OpenDB(filepath.Join(stateDir, "users.db"))
	if err != nil {
		g.stateError = err.Error()
		return g
	}
	builtins, err := mangajob.Definitions(a.configDir)
	var registry *tasks.Registry
	if err == nil {
		registry, err = tasks.LoadRegistry(a.configDir, builtins...)
	}
	if err != nil {
		log.Printf("Task registry disabled: %v", err)
		registry = tasks.DisabledRegistry(err)
	}
	router, err := server.NewRouter(g.libraries, db, stateDir, a.localToken, registry)
	if err != nil {
		g.stateError = errors.Join(err, db.Close()).Error()
		return g
	}
	router.ConfigureShortVideo(a.cfg.ShortVideoThumbnails, a.saveShortVideoThumbnails)
	if !a.cfg.StateInitialized {
		a.cfg.StateInitialized = true
		if err := config.Save(a.cfg); err != nil {
			a.cfg.StateInitialized = false
			g.stateError = errors.Join(err, router.Close(), db.Close()).Error()
			return g
		}
	}
	g.router, g.authDB, g.stateError = router, db, ""
	return g
}

func (a *Application) enter() (*generation, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.paused {
		return nil, false
	}
	a.active++
	snapshot := *a.current
	return &snapshot, true
}

func (a *Application) leave() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.active--
	if a.paused && a.active == 0 {
		close(a.drained)
	}
}

// ServeHTTP 在切换期间拒绝新业务请求；离线时仅保留受本机凭证保护的管理入口。
func (a *Application) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if isManagementPath(req.URL.Path) {
		if !a.authorize(w, req) {
			return
		}
		a.handleManagement(w, req)
		return
	}
	g, ok := a.enter()
	if !ok {
		http.Error(w, "service reconfiguration in progress", http.StatusServiceUnavailable)
		return
	}
	defer a.leave()
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(g.ctx, func() {
		cancel()
		controller := http.NewResponseController(w)
		// 先解除可能阻塞的网络读取，再关闭请求体；反过来会被慢速请求体卡住。
		if err := controller.SetReadDeadline(time.Now()); err != nil && !errors.Is(err, http.ErrNotSupported) {
			log.Printf("Cancel request read: %v", err)
		}
		if err := controller.SetWriteDeadline(time.Now()); err != nil && !errors.Is(err, http.ErrNotSupported) {
			log.Printf("Cancel response: %v", err)
		}
		if req.Body != nil {
			if err := req.Body.Close(); err != nil {
				log.Printf("Close cancelled request body: %v", err)
			}
		}
	})
	defer stop()
	defer cancel()
	if g.router == nil {
		if req.URL.Path == "/" || req.URL.Path == "/index.html" || len(req.URL.Path) > 8 && req.URL.Path[:8] == "/assets/" {
			web.Handler().ServeHTTP(w, req)
			return
		}
		http.Error(w, "global state offline; reconnect its library and use local recovery", http.StatusServiceUnavailable)
		return
	}
	g.router.ServeHTTP(w, req.WithContext(ctx))
}

func (a *Application) authorize(w http.ResponseWriter, req *http.Request) bool {
	g, ok := a.enter()
	if !ok {
		http.Error(w, "service reconfiguration in progress", http.StatusServiceUnavailable)
		return false
	}
	defer a.leave()
	middleware := auth.NewMiddleware(g.authDB, a.localToken)
	if middleware.IsTrustedLocal(req) {
		return true
	}
	if g.authDB == nil {
		http.Error(w, "local recovery credential required", http.StatusForbidden)
		return false
	}
	allowed := false
	middleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed = auth.UserFromContext(r.Context()).Role == auth.RoleAdmin && !auth.IsWANFromContext(r.Context())
		if !allowed {
			http.Error(w, "local or LAN administrator required", http.StatusForbidden)
		}
	})).ServeHTTP(w, req)
	return allowed
}

func (a *Application) pause(allowRunningTasks bool) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("application closed")
	}
	g := a.current
	if !allowRunningTasks && g.router != nil && !g.router.CanReconfigure() {
		a.mu.Unlock()
		return errors.New("stop running resource tasks before reconfiguring libraries")
	}
	a.paused = true
	a.drained = make(chan struct{})
	if a.active == 0 {
		close(a.drained)
	}
	drained := a.drained
	g.cancel()
	a.mu.Unlock()
	select {
	case <-drained:
		if !allowRunningTasks && g.router != nil && !g.router.CanReconfigure() {
			a.resumeWithFreshContext(g)
			return errors.New("a resource task started before requests drained; stop it before reconfiguring")
		}
		return nil
	case <-time.After(5 * time.Second):
		a.mu.Lock()
		g.ctx, g.cancel = context.WithCancel(context.Background())
		a.paused = false
		a.mu.Unlock()
		return errors.New("active requests did not drain; state remains at its original location")
	}
}

func closeGeneration(g *generation) error {
	g.cancel()
	var errs []error
	if g.router != nil {
		errs = append(errs, g.router.Close())
	}
	if g.authDB != nil {
		errs = append(errs, g.authDB.Close())
	}
	return errors.Join(errs...)
}

func (a *Application) resume(g *generation) {
	a.mu.Lock()
	a.current, a.paused = g, false
	a.mu.Unlock()
}

// WatchLibraries 检查挂载身份和全局状态是否仍在线，恢复时重建连接；不扫描媒体文件。
// 调用方负责启动此循环并通过 ctx 取消；仅状态改变或离线恢复时重建路由。
func (a *Application) WatchLibraries(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.checkLibraries(); err != nil {
				log.Printf("Library availability change: %v", err)
			}
		}
	}
}

func (a *Application) checkLibraries() error {
	a.operation.Lock()
	defer a.operation.Unlock()
	a.mu.Lock()
	g, closed := a.current, a.closed
	a.mu.Unlock()
	if closed {
		return nil
	}
	changed := false
	knownPaths := make(map[string]bool)
	for _, lib := range g.libraries.ListLibraries() {
		knownPaths[lib.RootPath] = true
		marker, err := library.ReadMarker(lib.RootPath)
		online := err == nil && (marker.ID == lib.ID || a.cfg.KnownLibrary(lib.RootPath) == nil)
		changed = changed || online != lib.IsOnline
	}
	for _, root := range append(append([]string{}, a.cfg.Libraries...), a.cfg.PrimaryLibrary) {
		if root == "" {
			continue
		}
		path, err := filepath.Abs(root)
		if err != nil || knownPaths[path] {
			continue
		}
		marker, err := library.ReadMarker(path)
		known := a.cfg.KnownLibrary(path)
		if err == nil && (known == nil || marker.ID == known.ID) {
			changed = changed || time.Now().After(a.retryAt)
		}
	}
	stateErr := a.cfg.CheckStateAvailability()
	if g.router != nil {
		changed = changed || stateErr != nil
	} else if stateErr == nil {
		changed = changed || time.Now().After(a.retryAt)
	}
	if !changed {
		return nil
	}
	if err := a.pause(true); err != nil {
		return err
	}
	closeErr := closeGeneration(g)
	a.resume(a.build())
	if a.current.router == nil {
		a.retryAt = time.Now().Add(30 * time.Second)
	} else {
		a.retryAt = time.Time{}
	}
	return closeErr
}

// Close 停止接收业务请求，取消并排空活动请求后关闭数据库，允许重复调用。
func (a *Application) Close() error {
	a.operation.Lock()
	defer a.operation.Unlock()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	g := a.current
	a.mu.Unlock()
	if err := a.pause(true); err != nil {
		return err
	}
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	return closeGeneration(g)
}

func cloneConfig(cfg *config.Config) (*config.Config, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var copy config.Config
	if err := json.Unmarshal(data, &copy); err != nil {
		return nil, fmt.Errorf("copy configuration: %w", err)
	}
	return &copy, nil
}

// saveShortVideoThumbnails 持久化缩略图开关。该回调在业务请求内执行，
// 不能等待正在暂停业务请求的配置切换，因此只尝试获取操作锁。
func (a *Application) saveShortVideoThumbnails(enabled bool) error {
	if !a.operation.TryLock() {
		return errors.New("configuration change in progress; retry later")
	}
	defer a.operation.Unlock()
	next, err := cloneConfig(a.cfg)
	if err != nil {
		return err
	}
	next.ShortVideoThumbnails = enabled
	if err := config.Save(next); err != nil && !config.WriteWasCommitted(err) {
		return err
	}
	a.cfg = next
	return nil
}
