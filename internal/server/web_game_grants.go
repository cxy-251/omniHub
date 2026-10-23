package server

import (
	"crypto/rand"
	"errors"
	"os"
	"sync"
	"time"
)

type webGameGrant struct {
	token                                      string
	libraryID, relative, category, sessionHash string
	deviceCredential                           bool
	deviceID                                   string
	clientID                                   string
	userID                                     int64
	folder                                     os.FileInfo
	expires                                    time.Time
	// gameID 是游戏清单里的编号（分类/目录名）；started/seen 是打开时间与最后一次确认还在玩的时间（游玩时长用）。
	gameID        string
	started, seen time.Time
}

type webGameGrants struct {
	mu     sync.Mutex
	values map[string]webGameGrant
	closed bool
	// onEnd 在授权结束（关闭、被新游戏替换、过期、登出）后调用一次，在锁外、单独的协程里执行。
	onEnd func(webGameGrant)
	ended []webGameGrant
}

// drop 移除授权并排队通知 onEnd；调用方持有锁，解锁后调用 report。
func (g *webGameGrants) drop(token string) {
	if value, ok := g.values[token]; ok {
		delete(g.values, token)
		g.ended = append(g.ended, value)
	}
}

func (g *webGameGrants) report() {
	g.mu.Lock()
	ended, onEnd := g.ended, g.onEnd
	g.ended = nil
	g.mu.Unlock()
	if onEnd != nil && len(ended) > 0 {
		go func() {
			for _, value := range ended {
				onEnd(value)
			}
		}()
	}
}

// touch 记下游戏页还开着（页面可见时每分钟一次）。release 为真时是关闭标签：最后一段最多再算 90 秒。
func (g *webGameGrants) touch(token string, release bool) {
	defer g.report()
	g.mu.Lock()
	defer g.mu.Unlock()
	value, ok := g.values[token]
	if !ok {
		return
	}
	now := time.Now()
	if release {
		if limit := value.seen.Add(90 * time.Second); now.After(limit) {
			now = limit
		}
		value.seen = now
		g.values[token] = value
		g.drop(token)
		return
	}
	value.seen = now
	g.values[token] = value
}

func newWebGameGrants() *webGameGrants { return &webGameGrants{values: make(map[string]webGameGrant)} }
func (g *webGameGrants) issue(value webGameGrant) (webGameGrant, error) {
	defer g.report()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return webGameGrant{}, errors.New("web game service closed")
	}
	now := time.Now()
	for token, value := range g.values {
		if !now.Before(value.expires) {
			g.drop(token)
		}
	}
	for token, previous := range g.values {
		if previous.userID == value.userID && previous.clientID == value.clientID {
			g.drop(token)
		}
	}
	owned := 0
	for _, previous := range g.values {
		if previous.userID == value.userID {
			owned++
		}
	}
	if owned >= 8 {
		return webGameGrant{}, errors.New("too many open games for this account")
	}
	if len(g.values) >= 64 {
		return webGameGrant{}, errors.New("too many open web games")
	}
	value.token = rand.Text()
	value.expires = now.Add(24 * time.Hour)
	value.started, value.seen = now, now
	g.values[value.token] = value
	return value, nil
}
func (g *webGameGrants) get(token string) (webGameGrant, bool) {
	defer g.report()
	g.mu.Lock()
	defer g.mu.Unlock()
	value, ok := g.values[token]
	if !ok || g.closed {
		return webGameGrant{}, false
	}
	if !time.Now().Before(value.expires) {
		g.drop(token)
		return webGameGrant{}, false
	}
	return value, true
}
func (g *webGameGrants) revoke(token string) {
	defer g.report()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.drop(token)
}
func (g *webGameGrants) close() { g.mu.Lock(); defer g.mu.Unlock(); g.closed = true; clear(g.values) }

// Credential transitions release the corresponding bounded iframe grants too,
// so revoked tabs do not occupy an account's eight-game admission budget.
func (g *webGameGrants) revokeCredential(hash string) {
	if hash == "" {
		return
	}
	defer g.report()
	g.mu.Lock()
	defer g.mu.Unlock()
	for token, value := range g.values {
		if value.sessionHash == hash {
			g.drop(token)
		}
	}
}
func (g *webGameGrants) revokeDevice(id string) {
	if id == "" {
		return
	}
	defer g.report()
	g.mu.Lock()
	defer g.mu.Unlock()
	for token, value := range g.values {
		if value.deviceID == id {
			g.drop(token)
		}
	}
}
func (g *webGameGrants) revokeUser(id int64) {
	defer g.report()
	g.mu.Lock()
	defer g.mu.Unlock()
	for token, value := range g.values {
		if value.userID == id {
			g.drop(token)
		}
	}
}
