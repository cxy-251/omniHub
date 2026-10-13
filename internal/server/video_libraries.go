package server

import (
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"omnihub/internal/shortvideo"
)

func readChannels(path string) (names []string, result error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	entries, err := f.ReadDir(20001)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > 20000 {
		return nil, errors.New("channel directory exceeds capacity")
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func (r *Router) listVideoChannels(w http.ResponseWriter, req *http.Request) {
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	seen := make(map[string]bool)
	for _, lib := range libs {
		path, err := r.libMgr.ResolveCategoryPath(lib.ID+":media_library/shortvideo", "media_library/shortvideo")
		if err != nil {
			http.Error(w, "video storage boundary invalid", 503)
			return
		}
		names, err := readChannels(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			http.Error(w, "video channels unavailable", 503)
			return
		}
		for _, name := range names {
			seen[name] = true
		}
		if len(seen) > 2048 {
			http.Error(w, "video channels exceed capacity", 503)
			return
		}
	}
	channels := make([]string, 0, len(seen))
	for name := range seen {
		channels = append(channels, name)
	}
	sort.Strings(channels)
	writeJSON(w, 200, channels)
}

func (r *Router) randomVideoLibrary(w http.ResponseWriter, req *http.Request) {
	channel := req.URL.Query().Get("channel")
	if channel == "" || len(channel) > 256 || strings.HasPrefix(channel, ".") || strings.ContainsAny(channel, "/\\:\x00") {
		http.Error(w, "invalid channel", 400)
		return
	}
	libs, ok := r.queryLibraries(w, req)
	if !ok {
		return
	}
	// Shuffle the media, then sample within the first nonempty channel. This
	// avoids scanning every library to compute weights for every matrix request.
	for _, index := range rand.Perm(len(libs)) {
		lib := libs[index]
		dir, err := r.libMgr.ResolveCategoryPath(lib.ID+":media_library/shortvideo/"+channel, "media_library/shortvideo")
		if err != nil {
			http.Error(w, "video storage boundary invalid", 503)
			return
		}
		for attempt := 0; attempt < 2; attempt++ {
			video, err := r.videoCatalog.Random(req.Context(), dir)
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, shortvideo.ErrNoVideos) {
				break
			}
			if err != nil {
				http.Error(w, "video catalog unavailable", 503)
				return
			}
			logical := lib.ID + ":media_library/shortvideo/" + filepath.ToSlash(filepath.Join(channel, video))
			path, err := r.libMgr.ResolveCategoryPath(logical, "media_library/shortvideo")
			if err == nil {
				info, err := os.Lstat(path)
				if err == nil && info.Mode().IsRegular() {
					writeJSON(w, 200, map[string]string{"path": logical, "library_id": lib.ID})
					return
				}
			}
			r.videoCatalog.Invalidate(dir)
		}
	}
	http.Error(w, "no videos in channel", 404)
}
