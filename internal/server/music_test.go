package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"omnihub/internal/audio"
	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/userstate"
)

func TestMusicChoicesAndRecycleBoundaries(t *testing.T) {
	if _, err := exec.LookPath("gio"); err != nil {
		t.Skip("GIO unavailable")
	}
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "desktop-data"))
	state, err := userstate.Open(filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	authDB, err := auth.OpenDB(filepath.Join(base, "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()
	manager := library.NewManager()
	if err := os.MkdirAll(filepath.Join(base, "library"), 0755); err != nil {
		t.Fatal(err)
	}
	lib, err := manager.AddLibrary(filepath.Join(base, "library"), true)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(lib.RootPath, audio.MusicRoot, "Artist")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "歌曲 + 1.mp3")
	original := []byte("fixture music, never user data")
	if err := os.WriteFile(file, original, 0644); err != nil {
		t.Fatal(err)
	}
	id := lib.ID + ":" + audio.MusicRoot + "/Artist/歌曲 + 1.mp3"
	router := &Router{libMgr: manager, authDB: authDB, userState: state, audioMetadata: audio.NewMetadataCache()}
	call := func(user *auth.User, action string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/media/audio/music/"+action, bytes.NewReader(data))
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, user))
		response := httptest.NewRecorder()
		router.handleMusic(response, req)
		return response
	}
	admin := &auth.User{ID: 1, Role: auth.RoleAdmin, Level: auth.LevelAdmin}
	reader := &auth.User{ID: 2, Role: auth.RoleUser, Level: 10}
	for _, rating := range []int{1, -1, 1} {
		if r := call(admin, "rating", map[string]any{"id": id, "rating": rating}); r.Code != 200 {
			t.Fatalf("rating: %d %s", r.Code, r.Body.String())
		}
	}
	one, _ := state.MusicRatings(context.Background(), 1)
	two, _ := state.MusicRatings(context.Background(), 2)
	if one[id] != 1 || len(two) != 0 {
		t.Fatal("ratings crossed accounts or choices were not exclusive")
	}
	if r := call(admin, "trash", map[string]any{"ids": []string{id}}); r.Code != 409 {
		t.Fatalf("liked song was eligible for trash: %d", r.Code)
	}
	if r := call(reader, "trash", map[string]any{"ids": []string{id}}); r.Code != 403 {
		t.Fatalf("ordinary account could delete files: %d", r.Code)
	}
	if r := call(admin, "rating", map[string]any{"id": id, "rating": -1}); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call(admin, "trash", map[string]any{"ids": []string{id, lib.ID + ":media_library/audio/music/../outside.mp3"}}); r.Code == 200 {
		t.Fatal("invalid mixed batch was accepted")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("batch validation moved a valid song before rejecting another path")
	}
	outside := filepath.Join(base, "outside.mp3")
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.mp3")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	linkID := lib.ID + ":" + audio.MusicRoot + "/Artist/linked.mp3"
	if err := state.SetMusicRating(context.Background(), 1, linkID, -1); err != nil {
		t.Fatal(err)
	}
	if r := call(admin, "trash", map[string]any{"ids": []string{linkID}}); r.Code == 200 {
		t.Fatal("symlink target was eligible for trash")
	}
	if r := call(admin, "trash", map[string]any{"ids": []string{id}}); r.Code != 200 {
		t.Fatalf("trash: %d %s", r.Code, r.Body.String())
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("file still exists after OS trash")
	}
	records, err := state.MusicTrashRecords(context.Background(), 1)
	if err != nil || len(records) != 1 || records[0].Status != "trashed" {
		t.Fatalf("missing recovery receipt: %+v %v", records, err)
	}
	if r := call(reader, "restore", map[string]string{"id": records[0].ID}); r.Code != 403 {
		t.Fatal("ordinary account could restore/delete physical files")
	}
	if err := os.WriteFile(file, []byte("new file"), 0644); err != nil {
		t.Fatal(err)
	}
	if r := call(admin, "restore", map[string]string{"id": records[0].ID}); r.Code != 409 {
		t.Fatal("restore overwrote an existing song")
	}
	if got, _ := os.ReadFile(file); string(got) != "new file" {
		t.Fatal("collision changed original destination")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	} // Test fixture, not user data.
	if r := call(admin, "restore", map[string]string{"id": records[0].ID}); r.Code != 200 {
		t.Fatalf("restore: %d %s", r.Code, r.Body.String())
	}
	if got, _ := os.ReadFile(file); !bytes.Equal(got, original) {
		t.Fatal("restored bytes changed")
	}
	if got, _ := os.ReadFile(outside); string(got) != "outside" {
		t.Fatal("outside file changed")
	}
	readOnly := library.NewManager()
	if _, err := readOnly.MountLibrary(lib.RootPath, true, true); err != nil {
		t.Fatal(err)
	}
	router.libMgr = readOnly
	if response := call(admin, "trash", map[string]any{"ids": []string{id}}); response.Code != 409 {
		t.Fatal("read-only library could be modified")
	}
	if got, _ := os.ReadFile(file); !bytes.Equal(got, original) {
		t.Fatal("read-only operation changed media")
	}
}
