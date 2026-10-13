package network

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
)

// ListenAvailable binds and retains the actual listener, avoiding a probe/bind port race.
func ListenAvailable(host string, port, attempts int) (net.Listener, int, error) {
	if host == "" || port < 1 || port > 65535 || attempts < 1 {
		return nil, 0, errors.New("invalid listener configuration")
	}
	for candidate := port; candidate < port+attempts && candidate <= 65535; candidate++ {
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(candidate)))
		if err == nil {
			return listener, candidate, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, 0, fmt.Errorf("listen: %w", err)
		}
	}
	return nil, 0, errors.New("no available service port in configured range")
}
