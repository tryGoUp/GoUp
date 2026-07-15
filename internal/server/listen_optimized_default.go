//go:build !linux

package server

import "net"

func listenOptimized(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}
