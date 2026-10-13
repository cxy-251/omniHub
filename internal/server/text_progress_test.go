package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/userstate"
)

func TestTextPositionReadOnlyAccountAndRevisionBoundaries(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner, err := db.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := db.CreateUser("reader", "test-password", "user", 99)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(reader.ID, "reader-session", time.Hour); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := library.NewManager().AddLibrary(root, false); err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(root, "media_library", "docs", "book.txt")
	if err := os.WriteFile(physical, []byte("one\ntwo\nthree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	lib, err := manager.MountLibrary(root, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	r, err := NewRouter(manager, db, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	key := lib.ID + ":media_library/docs/book.txt"
	request := func(method, path string, data any, other bool) *httptest.ResponseRecorder {
		body, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1234"
		if other {
			req.Header.Set("Authorization", "Bearer reader-session")
		} else {
			req.Header.Set("X-Omni-Local-Token", "token")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	get := func(other bool) *httptest.ResponseRecorder {
		return request("GET", "/api/media/text/progress?document_id="+url.QueryEscape(key), nil, other)
	}
	var response struct {
		Position *userstate.TextPosition `json:"position"`
		Version  string                  `json:"file_version"`
		CanSave  bool                    `json:"can_save"`
	}
	w := get(false)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || !response.CanSave || response.Version == "" || response.Position != nil {
		t.Fatalf("initial position: %d %s", w.Code, w.Body.String())
	}
	position := userstate.TextPosition{DocumentID: key, StartLine: 2, ByteOffset: 4, FileVersion: response.Version}
	if w := request("POST", "/api/media/text/progress", position, false); w.Code != 200 {
		t.Fatalf("read-only position: %d %s", w.Code, w.Body.String())
	}
	w = get(false)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Position == nil || response.Position.ByteOffset != 4 || response.Position.StartLine != 2 {
		t.Fatal("position lost")
	}
	w = get(true)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Position != nil {
		t.Fatal("position leaked to another account")
	}
	position.UserID = owner.ID
	position.StartLine = 3
	position.ByteOffset = 8
	if w := request("POST", "/api/media/text/progress", position, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = get(false)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Position == nil || response.Position.StartLine != 2 {
		t.Fatal("forged user_id overwrote owner's position")
	}
	position.ByteOffset = 999
	if w := request("POST", "/api/media/text/progress", position, false); w.Code != 400 {
		t.Fatal("out-of-file cursor accepted")
	}
	if err := os.WriteFile(physical, []byte("changed document\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w = get(false)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Position != nil {
		t.Fatal("old file position applied to replacement text")
	}
	position.ByteOffset = 4
	if w := request("POST", "/api/media/text/progress", position, false); w.Code != 409 {
		t.Fatal("stale revision saved after file change")
	}
}
