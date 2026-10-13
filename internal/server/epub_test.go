package server

import (
	"archive/zip"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omnihub/internal/auth"
	"omnihub/internal/library"
	"omnihub/internal/text"
)

func TestEpubChapterAssetsStayInOwningLibraryAndKeepSandbox(t *testing.T) {
	state := t.TempDir()
	db, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := library.NewManager()
	contents := make(map[string]string)
	for _, content := range []string{"First book", "Second book"} {
		root := t.TempDir()
		if _, err := library.NewManager().AddLibrary(root, false); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(root, "media_library", "docs", "same.epub"))
		if err != nil {
			t.Fatal(err)
		}
		w := zip.NewWriter(f)
		for name, body := range map[string]string{
			"META-INF/container.xml": `<container><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`,
			"OPS/book.opf":           `<package><metadata><title>Title</title></metadata><manifest><item id="one" href="chapters/第一章.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="one"/></spine></package>`,
			"OPS/chapters/第一章.xhtml": `<html><head><link rel="stylesheet" href="../styles/book.css"/></head><body>` + content + `<img src="../images/a.png"/><script>alert(1)</script></body></html>`,
			"OPS/styles/book.css":    "body { color: black }",
			"OPS/images/a.png":       "picture-" + content,
		} {
			entry, err := w.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
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
	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Omni-Local-Token", "token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, lib := range manager.ListLibraries() {
		logical := lib.ID + ":media_library/docs/same.epub"
		w := request("/api/media/text/epub/book?path=" + url.QueryEscape(logical))
		var book struct {
			text.EpubBook
			AssetBase   string `json:"asset_base"`
			LogicalPath string `json:"logical_path"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &book); err != nil || len(book.Chapters) != 1 || book.LogicalPath != logical {
			t.Fatalf("book: %d %s", w.Code, w.Body.String())
		}
		chapterURL := book.AssetBase + "OPS/chapters/" + url.PathEscape("第一章.xhtml")
		w = request(chapterURL)
		if w.Code != 200 || !strings.Contains(w.Body.String(), contents[lib.ID]) {
			t.Fatalf("wrong chapter: %d %s", w.Code, w.Body.String())
		}
		policy := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(policy, "sandbox allow-same-origin;") || !strings.Contains(policy, "default-src 'none'") || strings.Contains(policy, "allow-scripts") {
			t.Fatal("chapter executable sandbox")
		}
		base, err := url.Parse(chapterURL)
		if err != nil {
			t.Fatal(err)
		}
		for _, relative := range []string{"../images/a.png", "../styles/book.css"} {
			asset, _ := url.Parse(relative)
			w = request(base.ResolveReference(asset).String())
			if w.Code != 200 {
				t.Fatalf("relative asset failed: %d %s", w.Code, w.Body.String())
			}
			if strings.HasSuffix(relative, ".png") && w.Body.String() != "picture-"+contents[lib.ID] {
				t.Fatal("same-name book loaded wrong library image")
			}
		}
		if w := request(book.AssetBase + "OPS/script.js"); w.Code != 415 {
			t.Fatal("script asset exposed")
		}
	}
}
