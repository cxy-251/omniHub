package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"omnihub/internal/tasks"
)

func (r *Router) streamTaskLogs(w http.ResponseWriter, req *http.Request, task *tasks.Task) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	write := func(frame string) bool {
		if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return false
		}
		if _, err := fmt.Fprint(w, frame); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	send := func(entry tasks.LogEntry) bool {
		data, err := json.Marshal(entry)
		return err == nil && write("id: "+fmt.Sprint(entry.ID)+"\ndata: "+string(data)+"\n\n")
	}
	if !write(": connected\n\n") {
		return
	}
	channel, history := task.SubscribeLogs(ctx)
	for _, entry := range history {
		if !send(entry) {
			return
		}
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if !write(": heartbeat\n\n") {
				return
			}
		case entry, open := <-channel:
			if !open {
				write("event: end\ndata: {}\n\n")
				return
			}
			if !send(entry) {
				return
			}
		}
	}
}
