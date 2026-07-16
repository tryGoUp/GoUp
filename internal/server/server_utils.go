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

// Default timeouts applied when a site does not configure its own. They exist
// to protect an out-of-the-box deployment from slowloris-style attacks where a
// client trickles headers or a body to hold connections open indefinitely.
const (
	defaultReadTimeout       = 60 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)

// createHTTPServer creates an HTTP server with the given configuration and handler.
func createHTTPServer(conf config.SiteConfig, handler http.Handler) *http.Server {
	// ReadTimeout bounds the time to read the whole request (headers + body).
	// Default it so a slow client cannot pin a connection forever, but let a
	// site opt into a different value (0 disables it, e.g. for large uploads).
	readTimeout := defaultReadTimeout
	if conf.RequestTimeout > 0 {
		readTimeout = time.Duration(conf.RequestTimeout) * time.Second
	} else if conf.RequestTimeout < 0 {
		readTimeout = 0
	}

	// WriteTimeout stays opt-in: a global write deadline would break large
	// downloads, proxied streams and SSE.
	writeTimeout := time.Duration(0)
	if conf.RequestTimeout > 0 {
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

	s.ReadHeaderTimeout = defaultReadHeaderTimeout
	if conf.ReadHeaderTimeout > 0 {
		s.ReadHeaderTimeout = time.Duration(conf.ReadHeaderTimeout) * time.Second
	}
	s.IdleTimeout = defaultIdleTimeout
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
			// server.TLSConfig has already been populated by setupTLS with
			// either static certificates (SNI-selected) or an ACME
			// GetCertificate callback. Share the same certificate source with
			// the QUIC (h3) server.
			l.Infof("Serving %s on HTTPS port %d with HTTP/2 and HTTP/3 support", conf.Domain, conf.Port)

			h3 := &http3.Server{
				Addr:    fmt.Sprintf(":%d", conf.Port),
				Port:    conf.Port,
				Handler: server.Handler,
				TLSConfig: &tls.Config{
					MinVersion:     tls.VersionTLS12,
					Certificates:   server.TLSConfig.Certificates,
					GetCertificate: server.TLSConfig.GetCertificate,
				},
			}
			registerCloser(h3)

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
