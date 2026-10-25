package platform

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// 安卓没有 /etc/resolv.conf，不带 cgo 编译的 Go 程序会去问 [::1]:53，解析全部失败（2026-10-26 手机 Termux 实测）。
// 这里改用 Termux 的 $PREFIX/etc/resolv.conf 里的服务器，再加两个国内公共 DNS；每次重试换下一个，
// 某个服务器不通时下一次尝试就用别的。
func init() {
	servers := termuxNameservers()
	servers = append(servers, "223.5.5.5", "119.29.29.29")
	var next atomic.Uint32
	var d net.Dialer
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			s := servers[int(next.Add(1)-1)%len(servers)]
			return d.DialContext(ctx, network, net.JoinHostPort(s, "53"))
		},
	}
}

func termuxNameservers() []string {
	prefix := os.Getenv("PREFIX")
	if prefix == "" {
		prefix = "/data/data/com.termux/files/usr"
	}
	f, err := os.Open(filepath.Join(prefix, "etc", "resolv.conf"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var servers []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" && net.ParseIP(fields[1]) != nil {
			servers = append(servers, fields[1])
		}
	}
	return servers
}
