//go:build linux

package server

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// listenOptimized creates a TCP listener with SO_REUSEPORT and TCP_FASTOPEN optimizations.
func listenOptimized(addr string) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
				unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_FASTOPEN, 256)
			})
		},
	}
	return lc.Listen(context.Background(), "tcp", addr)
}
