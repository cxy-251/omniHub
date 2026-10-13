package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/config"
	"omnihub/internal/library"
	"omnihub/internal/state"
)

type confirmation struct {
	Token       string
	LibraryID   string
	Source      string
	Destination string
	Expires     time.Time
}

func isManagementPath(path string) bool {
	switch path {
	case "/api/library/manage", "/api/library/add", "/api/library/remove", "/api/library/refresh", "/api/library/categories", "/api/library/primary/preview", "/api/library/primary/commit":
		return true
	}
	return false
}

func (a *Application) handleManagement(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/api/library/manage" {
		if req.Method != http.MethodGet {
			methodError(w, http.MethodGet)
			return
		}
	} else {
		if req.Method != http.MethodPost {
			methodError(w, http.MethodPost)
			return
		}
		if origin := req.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != req.Host || (u.Scheme != "http" && u.Scheme != "https") {
				http.Error(w, "cross-origin state change forbidden", http.StatusForbidden)
				return
			}
		}
	}
	a.operation.Lock()
	defer a.operation.Unlock()
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		http.Error(w, "application closed", http.StatusServiceUnavailable)
		return
	}
	switch req.URL.Path {
	case "/api/library/manage":
		a.managementSnapshot(w)
	case "/api/library/add":
		var body struct {
			Path     string `json:"path"`
			ReadOnly bool   `json:"read_only"`
		}
		if !decodeBody(w, req, &body) {
			return
		}
		if !filepath.IsAbs(body.Path) || len(body.Path) > 4096 {
			http.Error(w, "absolute library root required", http.StatusBadRequest)
			return
		}
		root, err := filepath.EvalSymlinks(filepath.Clean(body.Path))
		if err != nil {
			http.Error(w, "library directory unavailable", http.StatusBadRequest)
			return
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			http.Error(w, "library root must be a directory", http.StatusBadRequest)
			return
		}
		for _, existing := range a.current.libraries.ListLibraries() {
			resolved, err := filepath.EvalSymlinks(existing.RootPath)
			if existing.RootPath == root || err == nil && resolved == root {
				http.Error(w, "library already configured", http.StatusConflict)
				return
			}
		}
		next, err := cloneConfig(a.cfg)
		if err != nil {
			managementError(w, err)
			return
		}
		// 验证标识但不把第一次新增库自动设为主库，初始化账号仍保留原位置。
		probe, err := library.NewConfiguredManager(a.cfg.CategoryDirectories)
		if err != nil {
			managementError(w, err)
			return
		}
		lib, err := probe.MountLibrary(root, false, body.ReadOnly)
		if err != nil {
			managementError(w, err)
			return
		}
		next.RememberLibrary(lib)
		next.Libraries = append(next.Libraries, root)
		if body.ReadOnly {
			next.ReadOnlyLibraries = append(next.ReadOnlyLibraries, root)
		}
		if err := a.reconfigure(next); err != nil {
			managementError(w, err)
			return
		}
		a.managementSnapshot(w)
	case "/api/library/remove":
		var body struct {
			LibraryID string `json:"library_id"`
		}
		if !decodeBody(w, req, &body) {
			return
		}
		lib := a.findLibrary(body.LibraryID)
		if lib == nil {
			http.Error(w, "library not found", http.StatusNotFound)
			return
		}
		next, err := cloneConfig(a.cfg)
		if err != nil {
			managementError(w, err)
			return
		}
		next.Libraries = withoutPath(next.Libraries, lib.RootPath)
		next.ReadOnlyLibraries = withoutPath(next.ReadOnlyLibraries, lib.RootPath)
		if samePath(next.PrimaryLibrary, lib.RootPath) {
			next.PrimaryLibrary = ""
		}
		delete(next.LibraryIdentities, lib.RootPath)
		if err := a.reconfigure(next); err != nil {
			managementError(w, err)
			return
		}
		a.managementSnapshot(w)
	case "/api/library/refresh":
		if err := a.reconfigure(a.cfg); err != nil {
			managementError(w, err)
			return
		}
		a.managementSnapshot(w)
	case "/api/library/categories":
		var body struct {
			Directories []string `json:"directories"`
		}
		if !decodeBody(w, req, &body) {
			return
		}
		if err := library.ValidateCategories(body.Directories); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		next, err := cloneConfig(a.cfg)
		if err != nil {
			managementError(w, err)
			return
		}
		active := make(map[string]bool, len(body.Directories))
		for _, dir := range body.Directories {
			active[dir] = true
		}
		disabled := make(map[string]bool)
		for _, dir := range append(append([]string{}, a.cfg.DisabledCategories...), a.cfg.CategoryDirectories...) {
			if !active[dir] {
				disabled[dir] = true
			}
		}
		next.CategoryDirectories = append([]string{}, body.Directories...)
		next.DisabledCategories = nil
		for dir := range disabled {
			next.DisabledCategories = append(next.DisabledCategories, dir)
		}
		sort.Strings(next.DisabledCategories)
		if err := a.reconfigure(next); err != nil {
			managementError(w, err)
			return
		}
		a.managementSnapshot(w)
	case "/api/library/primary/preview":
		var body struct {
			LibraryID string `json:"library_id"`
		}
		if !decodeBody(w, req, &body) {
			return
		}
		lib, err := a.migrationTarget(body.LibraryID)
		if err != nil {
			managementError(w, err)
			return
		}
		token, err := auth.GenerateSecureToken()
		if err != nil {
			managementError(w, err)
			return
		}
		preview := &confirmation{Token: token, LibraryID: lib.ID, Source: a.cfg.StateDirectory, Destination: filepath.Join(lib.RootPath, library.DataDirName), Expires: time.Now().Add(2 * time.Minute)}
		a.confirmation = preview
		respond(w, map[string]any{"confirmation_token": token, "source": preview.Source, "destination": preview.Destination, "expires_at": preview.Expires, "message": "即将暂停当前播放并迁移账号、会话和全局进度至新主库。模块数据库留在各自资源库；跨盘复制后保留旧副本。目标已有全局数据时拒绝覆盖。"})
	case "/api/library/primary/commit":
		var body struct {
			LibraryID         string `json:"library_id"`
			ConfirmationToken string `json:"confirmation_token"`
		}
		if !decodeBody(w, req, &body) {
			return
		}
		preview := a.confirmation
		if preview == nil || body.ConfirmationToken != preview.Token || body.LibraryID != preview.LibraryID || time.Now().After(preview.Expires) || a.cfg.StateDirectory != preview.Source {
			http.Error(w, "migration confirmation missing or expired", http.StatusConflict)
			return
		}
		a.confirmation = nil
		if err := a.migrate(preview); err != nil {
			managementError(w, err)
			return
		}
		a.managementSnapshot(w)
	}
}

func (a *Application) findLibrary(id string) *library.Library {
	for _, lib := range a.current.libraries.ListLibraries() {
		if lib.ID == id {
			return lib
		}
	}
	return nil
}

func (a *Application) migrationTarget(id string) (*library.Library, error) {
	if a.current.router == nil {
		return nil, errors.New("reconnect the original global state before migration")
	}
	lib := a.findLibrary(id)
	if lib == nil || !lib.IsOnline || lib.ReadOnly {
		return nil, errors.New("migration requires an online writable library")
	}
	if a.cfg.StateDirectory == filepath.Join(lib.RootPath, library.DataDirName) {
		return nil, errors.New("global state is already in this library")
	}
	marker, err := library.ReadMarker(lib.RootPath)
	if err != nil {
		return nil, err
	}
	if marker.ID != lib.ID {
		return nil, errors.New("target library medium changed")
	}
	target := filepath.Join(lib.RootPath, library.DataDirName)
	entries, err := os.ReadDir(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, errors.New("target already contains global state")
	}
	return lib, nil
}

func (a *Application) migrate(preview *confirmation) error {
	lib, err := a.migrationTarget(preview.LibraryID)
	if err != nil {
		return err
	}
	if filepath.Join(lib.RootPath, library.DataDirName) != preview.Destination {
		return errors.New("migration target changed")
	}
	next, err := cloneConfig(a.cfg)
	if err != nil {
		return err
	}
	next.PrimaryLibrary, next.StateDirectory = lib.RootPath, preview.Destination
	next.StateLibraryRoot, next.StateLibraryID = lib.RootPath, lib.ID
	if err := a.pause(false); err != nil {
		return err
	}
	g := a.current
	if err := closeGeneration(g); err != nil {
		a.resume(a.build())
		return err
	}
	err = state.Migrate(a.journalPath(), state.Plan{Source: preview.Source, Destination: preview.Destination, Bootstrap: samePath(preview.Source, a.configDir)}, func() error {
		if err := config.Save(next); err != nil {
			if config.WriteWasCommitted(err) {
				a.cfg = next
			}
			return err
		}
		a.cfg = next
		return nil
	})
	a.resume(a.build())
	if err != nil {
		return err
	}
	if a.current.router == nil {
		return errors.New("migration committed; global state needs recovery: " + a.current.stateError)
	}
	return nil
}

func (a *Application) reconfigure(next *config.Config) error {
	if err := a.pause(false); err != nil {
		return err
	}
	a.confirmation = nil
	g := a.current
	saveErr := config.Save(next)
	if saveErr != nil && !config.WriteWasCommitted(saveErr) {
		a.resumeWithFreshContext(g)
		return saveErr
	}
	a.cfg = next
	if err := closeGeneration(g); err != nil {
		a.resume(a.build())
		return err
	}
	a.resume(a.build())
	return saveErr
}

func (a *Application) resumeWithFreshContext(g *generation) {
	g.ctx, g.cancel = context.WithCancel(context.Background())
	a.resume(g)
}

func (a *Application) managementSnapshot(w http.ResponseWriter) {
	respond(w, map[string]any{"libraries": a.current.libraries.ListLibraries(), "primary": a.current.libraries.GetPrimary(), "state_directory": a.cfg.StateDirectory, "state_ready": a.current.router != nil, "state_error": a.current.stateError, "category_directories": a.cfg.CategoryDirectories, "disabled_categories": a.cfg.DisabledCategories})
}

func samePath(a, b string) bool {
	return library.SameRoot(a, b)
}

func withoutPath(paths []string, root string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if !samePath(path, root) {
			result = append(result, path)
		}
	}
	return result
}

func decodeBody(w http.ResponseWriter, req *http.Request, target any) bool {
	req.Body = http.MaxBytesReader(w, req.Body, 64<<10)
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "unexpected trailing request data", http.StatusBadRequest)
		return false
	}
	return true
}

func methodError(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
func managementError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusConflict)
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("Write library management response: %v", err)
	}
}
