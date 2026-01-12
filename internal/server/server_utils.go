package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/sys/unix"
)

// createHTTPServer creates an HTTP server with the given configuration and handler.
func createHTTPServer(conf config.SiteConfig, handler http.Handler) *http.Server {
	readTimeout := time.Duration(0)
	writeTimeout := time.Duration(0)
	if conf.RequestTimeout >= 0 {
		readTimeout = time.Duration(conf.RequestTimeout) * time.Second
		writeTimeout = time.Duration(conf.RequestTimeout) * time.Second
	}

	s := &http.Server{
		Addr:         fmt.Sprintf(":%d", conf.Port),
		Handler:      handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		TLSConfig: &tls.Config{
			NextProtos: []string{"h3", "h2", "http/1.1"},
		},
	}

	if conf.ReadHeaderTimeout > 0 {
		s.ReadHeaderTimeout = time.Duration(conf.ReadHeaderTimeout) * time.Second
	}
	if conf.IdleTimeout > 0 {
		s.IdleTimeout = time.Duration(conf.IdleTimeout) * time.Second
	}
	if conf.MaxHeaderBytes > 0 {
		s.MaxHeaderBytes = conf.MaxHeaderBytes
	}

	return s
}

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

// startServerInstance starts the HTTP server instance.
func startServerInstance(server *http.Server, conf config.SiteConfig, l *logger.Logger) {
	go func() {
		if conf.SSL.Enabled {
			// SSL/TLS configuration
			if _, err := os.Stat(conf.SSL.Certificate); os.IsNotExist(err) {
				l.Errorf("SSL certificate not found for %s: %v", conf.Domain, err)
				return
			}
			if _, err := os.Stat(conf.SSL.Key); os.IsNotExist(err) {
				l.Errorf("SSL key not found for %s: %v", conf.Domain, err)
				return
			}

			l.Infof("Serving %s on HTTPS port %d with HTTP/2 and HTTP/3 support", conf.Domain, conf.Port)

			// HTTP/1.1 and HTTP/2 server are also started to keep compatibility
			// with clients that do not support HTTP/3
			go func() {
				if err := server.ListenAndServeTLS(conf.SSL.Certificate, conf.SSL.Key); err != nil && err != http.ErrServerClosed {
					l.Errorf("HTTP/1.1 and HTTP/2 server error for %s: %v", conf.Domain, err)
				}
			}()

			quicAddr := fmt.Sprintf(":%d", conf.Port)
			err := http3.ListenAndServeQUIC(quicAddr, conf.SSL.Certificate, conf.SSL.Key, server.Handler)
			if err != nil && err != http.ErrServerClosed {
				l.Errorf("HTTP/3 server error for %s: %v", conf.Domain, err)
			}
		} else {
			l.Infof("Serving on HTTP port %d", conf.Port)
			ln, err := listenOptimized(server.Addr)
			if err != nil {
				l.Errorf("Error listening on port %d: %v", conf.Port, err)
				return
			}
			if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
				l.Errorf("Server error on port %d: %v", conf.Port, err)
			}
		}
	}()
}
