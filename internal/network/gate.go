package network

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// GateConfig 是对外服务的范围。HomeOnly 为 false 时监听 Host 的所有地址（以前的行为）；
// 为 true 时本机地址（127.0.0.1）一直可用，局域网地址只在 Trusted 里的网络上开放，
// 连到别的 Wi-Fi 或移动数据时自动关闭，回到可信网络后下一次检查时恢复。手机随身带出门时用。
type GateConfig struct {
	Host     string
	HomeOnly bool
	Trusted  []string // 可信网络的指纹（见 NetState）
}

// NetState 是当前网络：本机的局域网地址和网络指纹（取自路由器的 UPnP 设备标识），取不到时为空。
type NetState struct {
	IP          string `json:"ip"`
	Fingerprint string `json:"fingerprint"`
}

// Gate 管理服务的监听端口，按网络状态开启或关闭局域网监听。
// 安卓会在应用受限或网络变化时作废应用的套接字（z6x hub 2026-10-08 手机实测：accept 返回 EINVAL，
// 进程还在却不再监听），所以监听意外结束时不退出，下一次检查时重新监听。
type Gate struct {
	port    int
	handler func() *http.Server
	probe   func() NetState // 测试时替换
	// OnExternal 在局域网监听开启或关闭时调用（main 用来开关 mDNS 广播）。
	OnExternal func(bool)

	mu      sync.Mutex
	cfg     GateConfig
	ext     *gateListener // 局域网（HomeOnly 时只在可信网络的地址上；否则 Host 的所有地址）
	lo      *gateListener // HomeOnly 时本机地址上一直开着的监听
	bound   string        // 局域网监听应在的地址（ext 为 nil 时没有对外服务）
	reason  string        // 没有对外服务的原因
	missing int           // 连续取不到网络指纹的次数
	net     NetState      // 最近一次检查到的网络
}

type gateListener struct {
	ln     net.Listener
	srv    *http.Server
	closed bool // 是 Gate 主动关闭的
	done   bool // 已结束（主动关闭或被系统作废）
}

// NewGate 创建监听管理；newServer 每次监听时调用，返回一个新的 http.Server（关闭后的 Server 不能再用）。
func NewGate(cfg GateConfig, port int, newServer func() *http.Server) *Gate {
	return &Gate{cfg: cfg, port: port, handler: newServer, probe: ProbeNetwork}
}

// Start 立即检查一次并开始监听；之后每 interval 检查一次，直到 stop 关闭。首次监听失败时返回错误。
func (g *Gate) Start(interval time.Duration, stop <-chan struct{}) error {
	if err := g.check(); err != nil {
		g.Close()
		return err
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if err := g.check(); err != nil {
					log.Printf("network gate: %v", err)
				}
			}
		}
	}()
	return nil
}

// SetConfig 换成新的范围并立即检查一次。
func (g *Gate) SetConfig(cfg GateConfig) error {
	g.mu.Lock()
	g.cfg = cfg
	g.missing = 0
	g.mu.Unlock()
	return g.check()
}

// GateStatus 是给设置页看的状态。
type GateStatus struct {
	HomeOnly bool     `json:"home_only"`
	Serving  string   `json:"serving"` // 正在对外服务的地址，空表示没有
	Reason   string   `json:"reason,omitempty"`
	Current  NetState `json:"current"`
	Trusted  bool     `json:"trusted"` // 当前网络是否可信
}

// Status 返回当前状态；fresh 为 true 时先重新检查一次网络。
func (g *Gate) Status(fresh bool) GateStatus {
	if fresh {
		g.mu.Lock()
		homeOnly := g.cfg.HomeOnly
		g.mu.Unlock()
		if homeOnly {
			if err := g.check(); err != nil {
				log.Printf("network gate: %v", err)
			}
		} else { // 不限网络时平时不检查网络，设置页要看（先信任再打开）时才读
			st := g.probe()
			g.mu.Lock()
			g.net = st
			g.mu.Unlock()
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return GateStatus{
		HomeOnly: g.cfg.HomeOnly, Serving: g.serving(), Reason: g.reason, Current: g.net,
		Trusted: g.net.Fingerprint != "" && slices.Contains(g.cfg.Trusted, g.net.Fingerprint),
	}
}

// Current 重新读取当前网络（信任当前网络时用）。
func (g *Gate) Current() NetState { return g.probe() }

// want 按网络状态决定局域网监听的地址：IP、Host（不限网络时）或空（不监听）及原因。
func (g *Gate) want() (string, string) {
	g.mu.Lock()
	cfg, bound := g.cfg, g.bound
	g.mu.Unlock()
	if !cfg.HomeOnly {
		return cfg.Host, ""
	}
	st := g.probe()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.net = st
	switch {
	case st.IP == "":
		return "", "没有连接局域网（Wi-Fi）"
	// 路由器的 UPnP 偶尔一次不回应（z6x hub 手机上约每小时一次）：地址没变时继续服务，连续 3 次取不到才停
	case st.Fingerprint == "" && st.IP == bound && g.missing < 2:
		g.missing++
		return st.IP, ""
	case st.Fingerprint == "":
		g.missing = 0
		return "", "读不到当前网络的标识（路由器没有回应 UPnP）"
	case !slices.Contains(cfg.Trusted, st.Fingerprint):
		g.missing = 0
		return "", "当前网络不是可信网络"
	}
	g.missing = 0
	return st.IP, ""
}

func (g *Gate) check() error {
	addr, reason := g.want()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reason = reason
	var errs []error
	wantExt := addr != "" || !g.cfg.HomeOnly
	was := g.ext != nil
	// 先关不再需要的监听，再开新的：从「所有地址」换成「本机 + 局域网地址」时端口才不冲突
	if g.ext != nil && (!wantExt || addr != g.bound || g.ext.done) {
		if g.ext.done && !g.ext.closed {
			log.Printf("network gate: listener on %s:%d was closed by the system, listening again", g.bound, g.port)
		}
		g.ext.close()
		g.ext = nil
	}
	needLo := g.cfg.HomeOnly && addr != "127.0.0.1"
	if g.lo != nil && (!needLo || g.lo.done) {
		g.lo.close()
		g.lo = nil
	}
	if needLo && g.lo == nil {
		l, err := g.listen("127.0.0.1")
		if err != nil {
			errs = append(errs, err)
		}
		g.lo = l
	}
	if wantExt && g.ext == nil {
		l, err := g.listen(addr)
		if err != nil {
			errs = append(errs, err)
		} else {
			g.ext = l
		}
	}
	g.bound = addr
	if now := g.ext != nil; now != was {
		if now {
			log.Printf("network gate: serving on %s:%d", g.serving(), g.port)
		} else {
			log.Printf("network gate: stopped serving the network: %s", reason)
		}
		if g.OnExternal != nil {
			go g.OnExternal(now)
		}
	}
	return errors.Join(errs...)
}

// serving 返回正在对外服务的地址（"*" 表示所有地址），没有时为空。调用时持有 g.mu。
func (g *Gate) serving() string {
	switch {
	case g.ext == nil:
		return ""
	case g.bound == "":
		return "*"
	}
	return g.bound
}

// listen 在 host:port 上监听并开始服务；结束时（主动关闭或被系统作废）标记 done。调用时持有 g.mu。
func (g *Gate) listen(host string) (*gateListener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(g.port)))
	if err != nil {
		return nil, fmt.Errorf("listen %s:%d: %w", host, g.port, err)
	}
	l := &gateListener{ln: ln, srv: g.handler()}
	go func() {
		err := l.srv.Serve(ln)
		g.mu.Lock()
		defer g.mu.Unlock()
		l.done = true
		if !l.closed && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("network gate: serve %s: %v", ln.Addr(), err)
		}
	}()
	return l, nil
}

func (l *gateListener) close() {
	l.closed = true
	l.srv.Close()
	l.ln.Close() // Serve 还没接手时 srv.Close 不会关闭它
}

// Close 关闭全部监听和连接。
func (g *Gate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, l := range []*gateListener{g.ext, g.lo} {
		if l != nil {
			l.close()
		}
	}
	g.ext, g.lo = nil, nil
}

// Servers 返回当前在用的 http.Server（退出时用来优雅关闭）。
func (g *Gate) Servers() []*http.Server {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []*http.Server
	for _, l := range []*gateListener{g.ext, g.lo} {
		if l != nil && !l.done {
			out = append(out, l.srv)
		}
	}
	return out
}

// ProbeNetwork 读取本机的局域网地址和网络指纹，不需要任何权限（安卓普通应用也能用，
// 安卓不许应用用 netlink 读网关硬件地址）：
//   - 地址：向公网地址建立 UDP「连接」（不发数据），系统按路由选出的本机地址；只接受私有地址，
//     移动数据网络的地址算作没有连接局域网；
//   - 指纹：用 SSDP 找局域网里的路由器（InternetGatewayDevice），取它描述文件里的 UDN（每台设备唯一）。
//     路由器关了 UPnP 时取不到指纹。
func ProbeNetwork() NetState {
	var st NetState
	c, err := net.Dial("udp4", "223.5.5.5:53")
	if err != nil {
		return st
	}
	ip := c.LocalAddr().(*net.UDPAddr).IP
	c.Close()
	if !ip.IsPrivate() {
		return st
	}
	st.IP = ip.String()
	if udn := gatewayUDN(ip, 2*time.Second); udn != "" {
		sum := sha256.Sum256([]byte("omnihub|" + udn))
		st.Fingerprint = hex.EncodeToString(sum[:8])
	}
	return st
}

// gatewayUDN 用 SSDP 查找与 local 同网段的路由器，返回它的 UDN。
func gatewayUDN(local net.IP, wait time.Duration) string {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local})
	if err != nil {
		return ""
	}
	defer conn.Close()
	msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\n" +
		"ST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
	if _, err := conn.WriteToUDP([]byte(msg), &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}); err != nil {
		return ""
	}
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return ""
		}
		loc := ""
		for _, line := range strings.Split(string(buf[:n]), "\r\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "location") {
				loc = strings.TrimSpace(v)
			}
		}
		// 只读回复者自己的描述地址，并且必须在同一网段，免得被引去访问别的主机
		u, err := url.Parse(loc)
		if err != nil || u.Hostname() != from.IP.String() || !sameSubnet24(from.IP, local) {
			continue
		}
		if udn := fetchUDN(loc); udn != "" {
			return udn
		}
	}
}

func sameSubnet24(a, b net.IP) bool {
	a4, b4 := a.To4(), b.To4()
	return a4 != nil && b4 != nil && a4[0] == b4[0] && a4[1] == b4[1] && a4[2] == b4[2]
}

var udnRe = regexp.MustCompile(`<UDN>\s*(uuid:[^<\s]+)\s*</UDN>`)

func fetchUDN(loc string) string {
	cl := &http.Client{Timeout: 2 * time.Second}
	resp, err := cl.Get(loc)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if m := udnRe.FindSubmatch(body); m != nil {
		return strings.ToLower(string(m[1]))
	}
	return ""
}
