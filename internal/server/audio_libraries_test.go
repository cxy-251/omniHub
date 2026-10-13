package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"omnihub/internal/audio"
	"omnihub/internal/auth"
	"omnihub/internal/library"
)

func TestNestedAudioLibrariesNaturalOrderingAndReadOnlyProgress(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateInitialAdmin("owner", "test-password"); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	contents := make(map[string]string)
	for _, content := range []string{"first library", "second library"} {
		root := t.TempDir()
		if _, err := library.NewManager().AddLibrary(root, false); err != nil {
			t.Fatal(err)
		}
		album := filepath.Join(root, "media_library", "audio", "standard", "same")
		if err := os.MkdirAll(album, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"10.mp3", "2.mp3"} {
			if err := os.WriteFile(filepath.Join(album, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		lib, err := manager.MountLibrary(root, false, true)
		if err != nil {
			t.Fatal(err)
		}
		contents[lib.ID] = content
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	r, err := NewRouter(manager, db, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := request(http.MethodGet, "/api/media/audio/albums", nil)
	var albums []*audio.Album
	if err := json.Unmarshal(w.Body.Bytes(), &albums); err != nil || len(albums) != 2 {
		t.Fatalf("albums: %d %s", w.Code, w.Body.String())
	}
	if albums[0].ID == albums[1].ID || albums[0].TrackCount != 2 {
		t.Fatal("album namespace or count incorrect")
	}
	var album audio.Album
	w = request(http.MethodGet, "/api/media/audio/album?path="+url.QueryEscape(albums[0].ID)+"&limit=1", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &album); err != nil || len(album.Tracks) != 1 || album.Tracks[0].Filename != "2.mp3" {
		t.Fatalf("track order: %s", w.Body.String())
	}
	id := album.Tracks[0].RelPath
	w = request(http.MethodPost, "/api/media/audio/progress", map[string]any{"audio_id": id, "offset": 42.5})
	if w.Code != 200 {
		t.Fatalf("read-only library should allow global progress: %s", w.Body.String())
	}
	w = request(http.MethodGet, "/api/media/audio/progress?audio_id="+url.QueryEscape(id), nil)
	var progress struct {
		Offset float64 `json:"offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil || progress.Offset != 42.5 {
		t.Fatalf("progress lost: %s", w.Body.String())
	}
	w = request(http.MethodGet, "/api/media/audio/stream?path="+url.QueryEscape(id), nil)
	if w.Code != 200 || w.Body.String() != contents[albums[0].LibraryID] {
		t.Fatal("audio routed incorrectly")
	}
	w = request(http.MethodPost, "/api/media/audio/progress", map[string]any{"audio_id": id, "offset": -1})
	if w.Code != 400 {
		t.Fatal("negative progress accepted")
	}
}
