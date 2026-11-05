package server

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestPhoneCoverRecycleRestoresAndDoesNotOverwrite(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("fake adb uses local sh")
	}
	root := t.TempDir()
	adb := filepath.Join(root, "adb")
	if err := os.WriteFile(adb, []byte("#!/bin/sh\nexec sh -c \"$4\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "media_library", "shortvideo")
	rel := "platform/creator/.thumbs"
	original := filepath.Join(dir, rel)
	os.MkdirAll(original, 0755)
	os.WriteFile(filepath.Join(original, "one.mp4.webp"), []byte("old cover"), 0644)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"worker":{"job":""},"libraries":[{"library_id":"test","stats":{"videos":1,"albums":0,"thumbnails":1,"thumbnail_failures":0}}]}`))
	}))
	defer s.Close()
	r := &Router{phoneADB: adb, phoneDir: dir, svJobs: newSVWorker()}
	p := &phoneThumbReview{Input: phoneThumbInput{Device: "test"}, Library: "test", Base: s.URL, Client: s.Client(), Expires: time.Now().Add(time.Hour), Summary: phoneThumbSummary{Trash: filepath.Join(root, ".omni_data", "trash", "shortvideo-thumbnails", "20261029-180000"), Remaining: 1}}
	names, sig, n, err := r.phoneThumbFiles(context.Background(), p, rel)
	if err != nil {
		t.Fatal(err)
	}
	p.Dirs = []phoneThumbDir{{Rel: rel, Signature: sig, Files: len(names), Bytes: n}}
	r.svJobs.phoneReview = p
	late := filepath.Join(original, "late.webp")
	os.WriteFile(late, []byte("changed after preview"), 0644)
	r.movePhoneThumbs(false)
	if r.svJobs.status().LastError == "" {
		t.Fatal("moved a directory that changed after preview")
	}
	if _, err := os.Stat(filepath.Join(original, "one.mp4.webp")); err != nil {
		t.Fatal("changed source was not preserved")
	}
	os.Remove(late)
	r.movePhoneThumbs(false)
	if err := r.svJobs.status().LastError; err != "" {
		t.Fatal(err)
	}
	backup := filepath.Join(p.Summary.Trash, rel, "one.mp4.webp")
	if data, err := os.ReadFile(backup); err != nil || string(data) != "old cover" {
		t.Fatal("missing recoverable cover", err)
	}
	// Simulate a host restart: rebuild the restore plan from the phone's tree.
	r.svJobs = newSVWorker()
	r.readPhoneThumbHistory("test")
	if r.svJobs.status().LastError != "" || r.svJobs.phoneReview == nil {
		t.Fatal("could not recover recycle history", r.svJobs.status().LastError)
	}
	os.MkdirAll(original, 0755)
	os.WriteFile(filepath.Join(original, "new.webp"), []byte("new"), 0644)
	r.movePhoneThumbs(true)
	if r.svJobs.status().LastError == "" {
		t.Fatal("restore overwrote a new .thumbs directory")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal("lost backup on restore conflict")
	}
	os.Remove(filepath.Join(original, "new.webp"))
	os.Remove(original)
	r.movePhoneThumbs(true)
	if err := r.svJobs.status().LastError; err != "" {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(original, "one.mp4.webp")); err != nil || string(data) != "old cover" {
		t.Fatal("restore failed", err)
	}
}

func TestPhoneCoversChecksEveryPipelinedEntry(t *testing.T) {
	var allowLast atomic.Bool
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") == "platform/creator/34.mp4" && !allowLast.Load() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpg.Bytes())
	}))
	defer s.Close()
	p := &phoneThumbReview{Library: "test", Base: s.URL, Client: s.Client()}
	var names []string
	for i := 0; i < 35; i++ {
		names = append(names, fmt.Sprintf("%d.mp4.webp", i))
	}
	r := &Router{}
	if ok, err := r.checkPhoneCovers(context.Background(), p, "platform/creator/.thumbs", names); err != nil || ok {
		t.Fatal("missing database cover did not block recycling", ok, err)
	}
	allowLast.Store(true)
	if ok, err := r.checkPhoneCovers(context.Background(), p, "platform/creator/.thumbs", names); err != nil || !ok {
		t.Fatal("complete cover set was rejected", ok, err)
	}
}
