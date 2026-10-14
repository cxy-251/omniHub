package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"omnihub/internal/auth"
	"omnihub/internal/games"
	"omnihub/internal/library"
	"path/filepath"
)

// Indexed native launches accept identities only; executable paths, argv,
// environment and save ownership always come from host metadata and auth.
func (r *Router) handleGameLaunch(w http.ResponseWriter, req *http.Request) {
	if !r.runtimeMu.TryRLock() {
		http.Error(w, "game indexing is busy", 409)
		return
	}
	defer r.runtimeMu.RUnlock()
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !r.gameControlAllowed(w, req) || !r.checkSectionAccess(w, req, "games", auth.TargetGUI) {
		return
	}
	user := auth.UserFromContext(req.Context())
	// Generic native engines do not honor OMNI_SAVE_DIR automatically. Until a
	// driver redirects their real saves, never expose owner saves to other users.
	if user.Role != auth.RoleAdmin {
		http.Error(w, "engine save redirection required for this account", 403)
		return
	}
	var input struct {
		LibraryID string `json:"library_id"`
		ID        string `json:"id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.LibraryID == "" || len(input.LibraryID) > 128 || input.ID == "" || len(input.ID) > 512 {
		http.Error(w, "invalid game identity", 400)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "unexpected trailing request data", 400)
		return
	}
	var selected *library.Library
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.ID == input.LibraryID {
			selected = lib
			break
		}
	}
	if selected == nil {
		http.Error(w, "library not found", 404)
		return
	}
	if !selected.IsOnline {
		http.Error(w, "library offline", 503)
		return
	}
	if selected.ReadOnly {
		http.Error(w, "game library is read-only", 403)
		return
	}
	directory, err := r.gameStorageDirectory(selected.RootPath)
	if err != nil {
		http.Error(w, "game index boundary invalid", 503)
		return
	}
	// A lookup must not create or migrate an index, even in a writable library.
	db, err := r.openGameSource(req.Context(), directory)
	if err != nil {
		http.Error(w, "game index unavailable", 503)
		return
	}
	game, queryErr := db.GetGame(req.Context(), input.ID)
	closeErr := db.Close()
	if errors.Is(queryErr, sql.ErrNoRows) && closeErr == nil {
		http.Error(w, "game not found", 404)
		return
	}
	if err := errors.Join(queryErr, closeErr); err != nil {
		http.Error(w, "game lookup failed", 503)
		return
	}
	if !game.Available {
		http.Error(w, "game is missing from its library", 404)
		return
	}
	if game.Target != "gui_only" {
		http.Error(w, "game requires web client", 403)
		return
	}
	if err := games.ValidateIndexGame(game); err != nil {
		http.Error(w, "invalid indexed game metadata", 422)
		return
	}
	category := "standalone_games/" + game.Category + "_games"
	if r.libMgr.IsCategoryDisabled(category) {
		http.Error(w, "game category disabled", 403)
		return
	}
	relative, err := games.ResolveIndexedDirectory(req.Context(), directory, game)
	if err != nil {
		http.Error(w, "indexed game directory unavailable", 422)
		return
	}
	gameDir, err := r.libMgr.ResolveCategoryPath(selected.ID+":standalone_games/"+relative, category)
	if err != nil {
		http.Error(w, "game directory boundary invalid", 503)
		return
	}
	logsDir := filepath.Join(directory, "logs")
	args, env, err := games.ParseLaunchFields(game.Args, game.Env)
	if err != nil {
		http.Error(w, "invalid indexed launch parameters", 422)
		return
	}
	// An appid requires a runtime driver; silently executing it natively would use
	// the wrong container and could touch the wrong saves.
	if game.ProtonAppID != "" {
		http.Error(w, "configured Proton driver required", 422)
		return
	}
	status, err := r.gameRuns.Start(req.Context(), selected.ID+":"+game.ID, games.LaunchOptions{GameDir: gameDir, ExePath: filepath.FromSlash(game.Exe), Args: args, Env: env, UserID: user.ID, IsAdmin: true, LogsDir: logsDir})
	if errors.Is(err, games.ErrRunBusy) {
		http.Error(w, "a native game is already running", 409)
		return
	}
	if err != nil {
		http.Error(w, "native game launch failed: "+err.Error(), 422)
		return
	}
	writeJSON(w, http.StatusCreated, status)
}
