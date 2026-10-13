package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/library"
)

func TestBufferedMediaReadAdmissionAndCanceledWaiter(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := NewRouter(library.NewManager(), db, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	handler := r.authMid.Handler(r.protectRoutes(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
		}
	})))
	request := func(ctx context.Context, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil).WithContext(ctx)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	var owners sync.WaitGroup
	for range 2 {
		owners.Add(1)
		go func() { defer owners.Done(); request(context.Background(), "/api/media/text/chunk") }()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("media owners did not enter")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan *httptest.ResponseRecorder, 1)
	go func() { waiter <- request(ctx, "/api/media/text/chunk") }()
	// A canceled third request cannot allocate a media response or remain queued.
	cancel()
	select {
	case result := <-waiter:
		if result.Code != 503 {
			t.Fatal("canceled waiter not rejected")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter stayed blocked")
	}
	select {
	case <-started:
		t.Fatal("third media reader bypassed budget")
	default:
	}
	// Streamed video uses no whole-file buffer and remains available while both
	// buffered read slots are occupied.
	streamed := make(chan struct{})
	go func() { request(context.Background(), "/api/media/shortvideo/stream"); close(streamed) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("streamed video blocked by buffered budget")
	}
	releaseOnce.Do(func() { close(release) })
	owners.Wait()
	select {
	case <-streamed:
	case <-time.After(time.Second):
		t.Fatal("streamed response did not finish")
	}
	if len(r.mediaReads) != 0 {
		t.Fatal("media read slots leaked after completion")
	}
}
