// Package network 实现局域网 mDNS 服务广播、IP 感知与 Cloudflare 隧道守护。
// 遵循 ARCHITECTURE.md 第 8 节规范。
package network

import (
	"fmt"
	"net"
	"strings"

	"github.com/grandcat/zeroconf"
)

// MDNSService 封装 mDNS 局域网服务广播（第 8.2.2 节）。
type MDNSService struct {
	server *zeroconf.Server
}

// StartMDNS 注册并启动 omnihub.local 服务广播。
func StartMDNS(port int) (*MDNSService, error) {
	if port <= 0 {
		return nil, fmt.Errorf("invalid port for mDNS: %d", port)
	}

	server, err := zeroconf.Register(
		"omnihub",
		"_http._tcp",
		"local.",
		port,
		[]string{"path=/"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register mDNS service: %w", err)
	}

	return &MDNSService{server: server}, nil
}

// Shutdown 关闭 mDNS 服务广播。
func (m *MDNSService) Shutdown() {
	if m.server != nil {
		m.server.Shutdown()
	}
}

// GetLocalIPs 获取本机全部有效的局域网 IPv4 地址列表（第 8.2.1 节）。
func GetLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 != nil && !strings.HasPrefix(ip4.String(), "169.254.") {
			ips = append(ips, ip4.String())
		}
	}
	return ips
}
