package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"omnihub/internal/auth"
	"omnihub/internal/library"
)

func TestReadOnlyUnindexedMangaPagingReadingAndGlobalProgress(t *testing.T) {
	state := t.TempDir()
	users, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer users.Close()
	if _, err = users.CreateInitialAdmin("owner", "test-password"); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	var roots []string
	for i := 0; i < 2; i++ {
		root := t.TempDir()
		roots = append(roots, root)
		if _, err = library.NewManager().AddLibrary(root, false); err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(root, "media_library/manga/nested")
		if err = os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		names := []string{"same.cbz"}
		if i == 1 {
			names = append(names, "second.cbz")
			if err = os.WriteFile(filepath.Join(directory, "zz-broken.cbz"), []byte("broken archive"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range names {
			file, err := os.Create(filepath.Join(directory, name))
			if err != nil {
				t.Fatal(err)
			}
			archive := zip.NewWriter(file)
			entries := map[string][]byte{"chapter/1.jpg": []byte(fmt.Sprintf("library-%d", i)), "ComicInfo.xml": []byte(fmt.Sprintf(`<ComicInfo><Title>Library %d</Title><Writer>Writer</Writer><Genre>Drama</Genre></ComicInfo>`, i))}
			var cover bytes.Buffer
			if err = jpeg.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
				t.Fatal(err)
			}
			entries["cover.jpg"] = cover.Bytes()
			for path, data := range entries {
				entry, err := archive.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = entry.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err = archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = manager.MountLibrary(root, false, true); err != nil {
			t.Fatal(err)
		}
	}
	if err = manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		result := map[string]string{}
		for _, root := range roots {
			err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				result[path] = fmt.Sprintf("%d/%d/%d", info.Size(), info.ModTime().UnixNano(), info.Mode())
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	before := snapshot()
	router, err := NewRouter(manager, users, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	request := func(method, path string, data any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	seen := map[string]bool{}
	for offset := 0; offset < 4; offset++ {
		response := request("GET", fmt.Sprintf("/api/media/manga/list?limit=1&offset=%d", offset), nil)
		var books []mangaItem
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &books) != nil || len(books) != 1 {
			t.Fatalf("discovery page %d: %d %s", offset, response.Code, response.Body.String())
		}
		book := books[0]
		if book.MetadataError != "" {
			if book.Title != "zz-broken" || !book.ReadOnly {
				t.Fatal("bad diagnostic item")
			}
			continue
		}
		if seen[book.LogicalPath] || !book.ReadOnly || book.Author != "Writer" || book.ChapterCount != 1 || len(book.Tags) != 1 {
			t.Fatalf("discovery identity/metadata failed: %+v", book)
		}
		seen[book.LogicalPath] = true
		if response.Header().Get("X-Has-More") != fmt.Sprint(offset < 3) {
			t.Fatal("cross-library pagination has-more incorrect")
		}
		page := request("GET", "/api/media/manga/page?path="+url.QueryEscape(book.LogicalPath)+"&entry=chapter/1.jpg", nil)
		if page.Code != 200 || page.Body.String() != fmt.Sprintf("library-%d", int(book.Title[len(book.Title)-1]-'0')) {
			t.Fatal("wrong owning archive")
		}
		cover := request("GET", "/api/media/manga/cover?path="+url.QueryEscape(book.LogicalPath), nil)
		if cover.Code != 200 {
			t.Fatal("unindexed cover unavailable")
		}
		progress := map[string]any{"manga_id": book.LogicalPath, "chapter_index": 0, "page_index": 0, "scroll_offset_ratio": 0.4, "position_mode": "page"}
		if saved := request("POST", "/api/media/manga/progress", progress); saved.Code != 200 {
			t.Fatalf("read-only global save: %d %s", saved.Code, saved.Body.String())
		}
		if loaded := request("GET", "/api/media/manga/progress?manga_id="+url.QueryEscape(book.LogicalPath), nil); loaded.Code != 200 || !bytes.Contains(loaded.Body.Bytes(), []byte(`"scroll_offset_ratio":0.4`)) {
			t.Fatal("global progress lost")
		}
	}
	if len(seen) != 3 {
		t.Fatal("damaged archive hid healthy books")
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("read-only browse modified library files, directories or timestamps")
	}
}
