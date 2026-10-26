package network

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

// 可信网络之外只开本机地址；回到可信网络开局域网地址；监听被系统作废后重新监听。
func TestGateHomeOnly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	var mu sync.Mutex
	state := NetState{IP: "127.0.0.2", Fingerprint: "other"} // 127.0.0.2 代替局域网地址
	g := NewGate(GateConfig{HomeOnly: true, Trusted: []string{"home"}}, port, func() *http.Server {
		return &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})}
	})
	g.probe = func() NetState { mu.Lock(); defer mu.Unlock(); return state }
	set := func(s NetState) { mu.Lock(); state = s; mu.Unlock() }
	stop := make(chan struct{})
	defer close(stop)
	if err := g.Start(time.Hour, stop); err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	open := func(host string) bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), time.Second)
		if err == nil {
			c.Close()
		}
		return err == nil
	}
	if !open("127.0.0.1") || open("127.0.0.2") {
		t.Fatal("untrusted network: want loopback only")
	}
	if st := g.Status(false); st.Serving != "" || st.Reason == "" {
		t.Fatalf("status = %+v", st)
	}

	set(NetState{IP: "127.0.0.2", Fingerprint: "home"})
	if err := g.check(); err != nil {
		t.Fatal(err)
	}
	if !open("127.0.0.1") || !open("127.0.0.2") {
		t.Fatal("trusted network: want loopback and LAN")
	}

	// 一次取不到指纹：地址没变，继续服务
	set(NetState{IP: "127.0.0.2"})
	_ = g.check()
	if !open("127.0.0.2") {
		t.Fatal("one missing fingerprint should keep serving")
	}
	set(NetState{IP: "127.0.0.2", Fingerprint: "home"})
	_ = g.check()

	// 系统作废监听（安卓上 accept 返回 EINVAL）：下一次检查重新监听
	g.mu.Lock()
	g.ext.ln.Close()
	g.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		g.mu.Lock()
		done := g.ext.done
		g.mu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := g.check(); err != nil {
		t.Fatal(err)
	}
	if !open("127.0.0.2") {
		t.Fatal("listener closed by the system was not reopened")
	}

	// 离开可信网络：局域网地址关闭
	set(NetState{IP: "127.0.0.3", Fingerprint: "cafe"})
	_ = g.check()
	if open("127.0.0.2") || !open("127.0.0.1") {
		t.Fatal("left trusted network: want loopback only")
	}
}
