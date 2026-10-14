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
	clientID                                   string
	userID                                     int64
	folder                                     os.FileInfo
	expires                                    time.Time
}

type webGameGrants struct {
	mu     sync.Mutex
	values map[string]webGameGrant
	closed bool
}

func newWebGameGrants() *webGameGrants { return &webGameGrants{values: make(map[string]webGameGrant)} }
func (g *webGameGrants) issue(value webGameGrant) (webGameGrant, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return webGameGrant{}, errors.New("web game service closed")
	}
	now := time.Now()
	for token, value := range g.values {
		if !now.Before(value.expires) {
			delete(g.values, token)
		}
	}
	for token, previous := range g.values {
		if previous.userID == value.userID && previous.clientID == value.clientID {
			delete(g.values, token)
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
	g.values[value.token] = value
	return value, nil
}
func (g *webGameGrants) get(token string) (webGameGrant, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	value, ok := g.values[token]
	if !ok || g.closed {
		return webGameGrant{}, false
	}
	if !time.Now().Before(value.expires) {
		delete(g.values, token)
		return webGameGrant{}, false
	}
	return value, true
}
func (g *webGameGrants) revoke(token string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.values, token)
}
func (g *webGameGrants) close() { g.mu.Lock(); defer g.mu.Unlock(); g.closed = true; clear(g.values) }
