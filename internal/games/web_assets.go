package games

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// WebEntry selects an indexed HTML, ROM or SWF entry, never a display-title path.
// HTML root/www index is preferred; otherwise one unambiguous payload is needed.
func WebEntry(directory string, game *Game) (kind, entry string, resultErr error) {
	if game == nil || game.Target != "web_only" {
		return "", "", errors.New("not a web game")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", "", err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	kind = "html"
	if game.Category == "retro" {
		kind = game.Category
	}
	matches := func(name string) bool {
		ext := strings.ToLower(path.Ext(name))
		if kind == "html" {
			return ext == ".html" || ext == ".htm"
		}
		switch ext {
		case ".nes", ".sfc", ".smc", ".gb", ".gbc", ".gba", ".n64", ".z64", ".v64", ".nds", ".md", ".gen", ".sms", ".gg", ".pce", ".zip":
			return true
		}
		return false
	}
	verify := func(name string) error {
		if !PublicWebAsset(name) || !matches(name) {
			return errors.New("invalid web game entry")
		}
		file, err := OpenWebAsset(root, name)
		if err != nil {
			return err
		}
		return file.Close()
	}
	if game.Exe != "" {
		if err := verify(game.Exe); err != nil {
			return "", "", err
		}
		return kind, game.Exe, nil
	}
	if kind == "html" {
		for _, name := range []string{"index.html", "www/index.html"} {
			if err := verify(name); err == nil {
				return kind, name, nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", "", err
			}
		}
		return "", "", errors.New("web game has no index.html or configured entry")
	}
	file, err := root.Open(".")
	if err != nil {
		return "", "", err
	}
	entries, readErr := file.ReadDir(2049)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, file.Close()); err != nil {
		return "", "", err
	}
	if len(entries) > 2048 {
		return "", "", errors.New("web payload selection exceeds capacity")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, candidate := range entries {
		if !candidate.Type().IsRegular() || !PublicWebAsset(candidate.Name()) || !matches(candidate.Name()) {
			continue
		}
		if entry != "" {
			return "", "", errors.New("multiple web payloads: configure exe in omni.json")
		}
		entry = candidate.Name()
	}
	if entry == "" {
		return "", "", errors.New("web payload not found")
	}
	return kind, entry, nil
}

// PublicWebAsset rejects private state, traversal and platform path syntax.
// The serving layer additionally rejects links at every path component.
func PublicWebAsset(name string) bool {
	if name == "" || len(name) > 4096 || path.Clean(name) != name || !filepath.IsLocal(filepath.FromSlash(name)) || strings.ContainsAny(name, "\\:\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		lower := strings.ToLower(part)
		if part == "" || strings.HasPrefix(part, ".") || lower == "save" || lower == "saves" || lower == "logs" || lower == "omni.json" {
			return false
		}
	}
	return true
}

// OpenWebAsset opens a public regular file below an existing root. Links at
// every component are rejected, and the opened file identity is rechecked.
func OpenWebAsset(root *os.Root, name string) (*os.File, error) {
	if !PublicWebAsset(name) {
		return nil, errors.New("private or invalid game asset")
	}
	var expected os.FileInfo
	var err error
	prefix := ""
	for _, part := range strings.Split(name, "/") {
		if prefix != "" {
			prefix += "/"
		}
		prefix += part
		expected, err = root.Lstat(filepath.FromSlash(prefix))
		if err != nil {
			return nil, err
		}
		if expected.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("game asset links are not allowed")
		}
	}
	if !expected.Mode().IsRegular() {
		return nil, errors.New("game asset must be a regular file")
	}
	file, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !os.SameFile(expected, opened) {
		return nil, errors.Join(errors.New("game asset changed during open"), file.Close())
	}
	return file, nil
}
