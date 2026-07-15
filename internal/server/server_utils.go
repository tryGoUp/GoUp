package server

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/quic-go/quic-go/http3"
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
		Handler:      withHealthCheck(handler),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		// "h3" is QUIC-only and must not be advertised over TCP; the HTTP/3
		// server manages its own ALPN.
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1"},
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

// startServerInstance starts the HTTP server instance.
func startServerInstance(server *http.Server, conf config.SiteConfig, l *logger.Logger) {
	registerServer(server)

	go func() {
		if conf.SSL.Enabled {
			// Load the keypair once and share it between the TCP (h1/h2) and
			// QUIC (h3) servers, so handshakes resume sessions from the same
			// config instead of re-reading certificate files.
			cert, err := tls.LoadX509KeyPair(conf.SSL.Certificate, conf.SSL.Key)
			if err != nil {
				l.Errorf("SSL certificate error for %s: %v", conf.Domain, err)
				return
			}
			server.TLSConfig.Certificates = []tls.Certificate{cert}

			l.Infof("Serving %s on HTTPS port %d with HTTP/2 and HTTP/3 support", conf.Domain, conf.Port)

			h3 := &http3.Server{
				Addr:    fmt.Sprintf(":%d", conf.Port),
				Port:    conf.Port,
				Handler: server.Handler,
				TLSConfig: &tls.Config{
					MinVersion:   tls.VersionTLS12,
					Certificates: []tls.Certificate{cert},
				},
			}

			// Advertise HTTP/3 to TCP clients via Alt-Svc so they can switch
			// to QUIC on the next request.
			baseHandler := server.Handler
			server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = h3.SetQUICHeaders(w.Header())
				baseHandler.ServeHTTP(w, r)
			})

			// HTTP/1.1 and HTTP/2 server are also started to keep compatibility
			// with clients that do not support HTTP/3
			go func() {
				if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
					l.Errorf("HTTP/1.1 and HTTP/2 server error for %s: %v", conf.Domain, err)
				}
			}()

			if err := h3.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
