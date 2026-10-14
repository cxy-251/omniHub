package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"omnihub/internal/library"
	"omnihub/internal/tasks"
)

func (r *Router) handleTasksCatalog(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	if problem := r.taskRegistry.Error(); problem != "" {
		http.Error(w, "task registry disabled: "+problem, 503)
		return
	}
	writeJSON(w, 200, map[string]any{"definitions": r.taskRegistry.List(), "tasks": r.taskMgr.List()})
}

func (r *Router) runRegisteredTask(w http.ResponseWriter, req *http.Request) {
	if problem := r.taskRegistry.Error(); problem != "" {
		http.Error(w, "task registry disabled: "+problem, 503)
		return
	}
	var input struct {
		Definition string            `json:"definition"`
		LibraryID  string            `json:"library_id"`
		Directory  string            `json:"directory"`
		Parameters map[string]string `json:"parameters"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || len(input.Directory) > 4096 || strings.ContainsRune(input.Directory, 0) {
		http.Error(w, "invalid registered task request", 400)
		return
	}
	definition, ok := r.taskRegistry.Lookup(input.Definition)
	if !ok {
		http.Error(w, "unregistered task", 400)
		return
	}
	var selected *library.Library
	for _, lib := range r.libMgr.ListLibraries() {
		if lib.ID == input.LibraryID {
			selected = lib
			break
		}
	}
	if selected == nil || !selected.IsOnline {
		http.Error(w, "library unavailable", 404)
		return
	}
	if definition.Write && selected.ReadOnly {
		http.Error(w, "task cannot write a read-only library", 403)
		return
	}
	category := definition.Category
	if r.libMgr.IsCategoryDisabled(category) {
		http.Error(w, "task category disabled", 403)
		return
	}
	// Only category-relative directories can be submitted. ResolveCategoryPath
	// validates traversal and resolves aliases before the manager compares locks.
	relative := filepath.ToSlash(filepath.Clean(input.Directory))
	if filepath.IsAbs(input.Directory) || relative == ".." || strings.HasPrefix(relative, "../") || strings.Contains(relative, "\\") {
		http.Error(w, "invalid task directory", 400)
		return
	}
	logical := selected.ID + ":" + category
	if relative != "." {
		logical += "/" + relative
	}
	directory, err := r.libMgr.ResolveCategoryPath(logical, category)
	if err != nil {
		http.Error(w, "invalid task storage boundary", 400)
		return
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		http.Error(w, "task directory unavailable", 404)
		return
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		http.Error(w, "task directory unavailable", 404)
		return
	}
	categoryRoot, err := r.libMgr.ResolveCategoryPath(selected.ID+":"+category, category)
	if err != nil {
		http.Error(w, "task category unavailable", 503)
		return
	}
	command, err := r.taskRegistry.Prepare(input.Definition, directory, input.Parameters, categoryRoot)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	task, err := r.taskMgr.RunCommand("task_"+time.Now().UTC().Format("20060102T150405.000000000"), command)
	if err != nil {
		status := 422
		if errors.Is(err, tasks.ErrResourceBusy) {
			status = 409
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, task)
}
