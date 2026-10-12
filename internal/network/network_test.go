package network

import (
	"net"
	"testing"
)

func TestFindAvailablePort(t *testing.T) {
	// 占用一个端口测试探测递增
	ln, err := net.Listen("tcp", ":18998")
	if err != nil {
		t.Skip("cannot bind test port, skip")
	}
	defer ln.Close()

	port, err := FindAvailablePort(18998, 5)
	if err != nil {
		t.Fatalf("failed to find port: %v", err)
	}

	if port <= 18998 {
		t.Errorf("expected port > 18998 due to occupation, got %d", port)
	}
}

func TestGetLocalIPs(t *testing.T) {
	ips := GetLocalIPs()
	// 本机正常应能查到至少一个 IPv4（或空，不应报错 panic）
	t.Logf("detected local IPs: %v", ips)
}
