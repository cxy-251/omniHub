package network

import (
	"net"
	"testing"
)

func TestListenAvailableRetainsFallbackPort(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	if port > 65525 {
		t.Skip("ephemeral port too near range boundary")
	}
	listener, selected, err := ListenAvailable("127.0.0.1", port, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if selected <= port {
		t.Fatalf("selected occupied port: %d", selected)
	}
	duplicate, err := net.Listen("tcp", listener.Addr().String())
	if err == nil {
		duplicate.Close()
		t.Fatal("fallback port was not held")
	}
}
