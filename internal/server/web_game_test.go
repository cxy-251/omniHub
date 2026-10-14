package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"omnihub/internal/auth"
	"omnihub/internal/games"
	"omnihub/internal/library"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebGameGrantsResolveOwningLibraryAndRevokeAccess(t *testing.T) {
	state := t.TempDir()
	authDB, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authDB.Close(); err != nil {
			t.Error(err)
		}
	})
	owner, err := authDB.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := authDB.CreateSession(owner.ID, "web-owner", time.Hour); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	roots := make([]*library.Library, 0, 2)
	snapshots := make(map[string][]byte)
	for _, content := range []string{"first", "second"} {
		lib, err := manager.AddLibrary(t.TempDir(), false)
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, lib)
		directory := filepath.Join(lib.RootPath, "standalone_games")
		folder := filepath.Join(directory, "rpg_games", "001 - Physical Folder")
		if err := os.MkdirAll(filepath.Join(folder, "www", "assets"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(folder, "save"), 0755); err != nil {
			t.Fatal(err)
		}
		html := `<h1>` + content + `</h1><script>
window.probe={};try{parent.document.body;probe.parentBlocked=false}catch{probe.parentBlocked=true}
Promise.all([fetch('assets/data.json').then(r=>r.text()).then(text=>probe.asset=text),fetch('/api/auth/status').then(()=>probe.apiBlocked=false).catch(()=>probe.apiBlocked=true)]).then(()=>{document.body.dataset.ready='yes'});
</script>`
		for name, value := range map[string]string{"www/index.html": html, "www/assets/data.json": content, "save/private": "secret", "omni.json": "secret"} {
			if err := os.WriteFile(filepath.Join(folder, filepath.FromSlash(name)), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
		db, err := games.OpenDB(directory)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertGames([]*games.Game{{ID: "001", Name: "Unrelated label", Category: "rpg", Target: "web_only", RelDir: "rpg_games/001 - Physical Folder"}, {ID: "002", Name: "Native", Category: "unity", Target: "gui_only"}}); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		index := filepath.Join(directory, "games.db")
		data, err := os.ReadFile(index)
		if err != nil {
			t.Fatal(err)
		}
		snapshots[index] = data
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(manager, authDB, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	request := func(method, route, body string, cookie bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, route, bytes.NewBufferString(body))
		req.RemoteAddr = "127.0.0.1:1234"
		if cookie {
			req.AddCookie(&http.Cookie{Name: "omni_token", Value: "web-owner"})
		} else if method != http.MethodDelete {
			req.Header.Set("Origin", "null")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	type opened struct{ Token, Kind, URL string }
	open := func(lib *library.Library, id, client string) opened {
		t.Helper()
		body := fmt.Sprintf(`{"library_id":%q,"id":%q,"client_id":%q}`, lib.ID, id, client)
		w := request("POST", "/api/games/web/open", body, true)
		var value opened
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil || w.Code != 201 {
			t.Fatalf("open: %d %s %v", w.Code, w.Body.String(), err)
		}
		return value
	}
	first := open(roots[0], "001", "tab-first")
	second := open(roots[1], "001", "tab-second")
	if first.Kind != "html" || second.Kind != "html" || first.Token == second.Token {
		t.Fatal("game identity collided")
	}
	for _, pair := range []struct {
		entry   opened
		content string
	}{{first, "first"}, {second, "second"}} {
		w := request("GET", pair.entry.URL, "", false)
		if w.Code != 200 || !strings.Contains(w.Body.String(), pair.content) || w.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") || strings.Contains(w.Header().Get("Content-Security-Policy"), "allow-same-origin") {
			t.Fatalf("scoped HTML/CORS isolation: %d %s", w.Code, w.Body.String())
		}
		asset := strings.TrimSuffix(pair.entry.URL, "index.html") + "assets/data.json"
		if w := request("GET", asset, "", false); w.Code != 200 || w.Body.String() != pair.content {
			t.Fatal("nested relative asset came from wrong library")
		}
		for _, suffix := range []string{"save/private", "omni.json", ".env", "www\\assets\\data.json"} {
			route := "/game-content/" + pair.entry.Token + "/" + suffix
			if w := request("GET", route, "", false); w.Code >= 200 && w.Code < 300 {
				t.Fatal("private or escaping asset exposed")
			}
		}
	}
	next := open(roots[0], "001", "tab-first")
	for i := 0; i < cap(router.webGameOpens); i++ {
		router.webGameOpens <- struct{}{}
	}
	if w := request("POST", "/api/games/web/open", "{}", true); w.Code != 503 {
		t.Fatal("entry lookup concurrency budget ignored")
	}
	for i := 0; i < cap(router.webGameOpens); i++ {
		<-router.webGameOpens
	}
	if w := request("GET", first.URL, "", false); w.Code != 403 {
		t.Fatal("old grant survived replacement in same tab")
	}
	for i := 0; i < cap(router.webGameReads); i++ {
		router.webGameReads <- struct{}{}
	}
	if w := request("GET", next.URL, "", false); w.Code != 503 {
		t.Fatal("asset concurrency budget ignored")
	}
	for i := 0; i < cap(router.webGameReads); i++ {
		<-router.webGameReads
	}
	link := filepath.Join(roots[0].RootPath, "standalone_games", "rpg_games", "001 - Physical Folder", "www", "assets", "link")
	if err := os.Symlink(filepath.Dir(link), link); err == nil {
		if w := request("GET", strings.TrimSuffix(next.URL, "index.html")+"assets/link/data.json", "", false); w.Code != 404 {
			t.Fatal("nested asset link followed")
		}
	}
	manager.SetCategoryDisabled("standalone_games/rpg_games", true)
	if w := request("GET", next.URL, "", false); w.Code != 403 {
		t.Fatal("disabled category grant stayed active")
	}
	manager.SetCategoryDisabled("standalone_games/rpg_games", false)
	if w := request("POST", "/api/games/web/open", fmt.Sprintf(`{"library_id":%q,"id":"002","client_id":"tab"}`, roots[0].ID), true); w.Code != 403 {
		t.Fatal("native game exposed to web")
	}
	if err := authDB.RevokeUserSessions(owner.ID); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", second.URL, "", false); w.Code != 403 {
		t.Fatal("revoked login retained game access")
	}
	if err := authDB.CreateSession(owner.ID, "web-owner", time.Hour); err != nil {
		t.Fatal(err)
	}
	close := request("DELETE", "/game-content/"+next.Token+"/", "", false)
	if close.Code != 204 {
		t.Fatalf("close: %d %s", close.Code, close.Body.String())
	}
	if w := request("GET", next.URL, "", false); w.Code != 403 {
		t.Fatal("closed game grant stayed active")
	}
	checkChromeWebGame(t, router)
	for index, before := range snapshots {
		after, err := os.ReadFile(index)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("web open/read changed index")
		}
	}
}

func TestWebGrantTableBoundsReplacementExpiryAndClose(t *testing.T) {
	grants := newWebGameGrants()
	first, err := grants.issue(webGameGrant{userID: 1, clientID: "same"})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := grants.issue(webGameGrant{userID: 1, clientID: "same"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := grants.get(first.token); ok {
		t.Fatal("replaced token remained usable")
	}
	if len(grants.values) != 1 {
		t.Fatal("same-tab reopen accumulated grants")
	}
	for id := 0; id < 63; id++ {
		if _, err := grants.issue(webGameGrant{userID: int64(id + 2), clientID: fmt.Sprint(id)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := grants.issue(webGameGrant{userID: 2, clientID: "extra"}); err == nil {
		t.Fatal("grant capacity ignored")
	}
	value := grants.values[replacement.token]
	value.expires = time.Now().Add(-time.Second)
	grants.values[replacement.token] = value
	if _, ok := grants.get(replacement.token); ok {
		t.Fatal("expired grant accepted")
	}
	if _, err := grants.issue(webGameGrant{userID: 2, clientID: "extra"}); err != nil {
		t.Fatal(err)
	}
	grants.close()
	if _, err := grants.issue(webGameGrant{userID: 1}); err == nil || len(grants.values) != 0 {
		t.Fatal("closed table retained or created grants")
	}
	quota := newWebGameGrants()
	for id := 0; id < 8; id++ {
		if _, err := quota.issue(webGameGrant{userID: 7, clientID: fmt.Sprint(id)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := quota.issue(webGameGrant{userID: 7, clientID: "extra"}); err == nil {
		t.Fatal("one account exhausted grants reserved for other accounts")
	}
	if _, err := quota.issue(webGameGrant{userID: 8, clientID: "other"}); err != nil {
		t.Fatal(err)
	}
}
