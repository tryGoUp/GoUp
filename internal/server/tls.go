package server

import (
	"crypto/tls"
	"net/http"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// setupTLS configures server.TLSConfig for the given site configs (one for a
// single-site server, many for a virtual host on the same port). It loads
// static certificates and, when any site requests ACME, wires an autocert
// manager that obtains and renews Let's Encrypt certificates (via TLS-ALPN-01
// on the TLS port). It returns false when no site enables TLS.
func setupTLS(server *http.Server, confs []config.SiteConfig, lg *logger.Logger) bool {
	var staticCerts []tls.Certificate
	var acmeHosts []string
	var acmeEmail, acmeCache string
	tlsWanted := false

	for _, c := range confs {
		if !c.SSL.Enabled {
			continue
		}
		tlsWanted = true
		if c.SSL.ACME {
			acmeHosts = append(acmeHosts, c.Domain)
			if c.SSL.Email != "" {
				acmeEmail = c.SSL.Email
			}
			if c.SSL.CacheDir != "" {
				acmeCache = c.SSL.CacheDir
			}
			continue
		}
		if c.SSL.Certificate == "" || c.SSL.Key == "" {
			lg.Errorf("SSL enabled for %s but certificate/key not set (and ACME off)", c.Domain)
			continue
		}
		cert, err := tls.LoadX509KeyPair(c.SSL.Certificate, c.SSL.Key)
		if err != nil {
			lg.Errorf("SSL certificate error for %s: %v", c.Domain, err)
			continue
		}
		staticCerts = append(staticCerts, cert)
	}

	if !tlsWanted {
		return false
	}

	server.TLSConfig.MinVersion = tls.VersionTLS12

	if len(acmeHosts) == 0 {
		// Static certificates only; the tls package selects by SNI.
		server.TLSConfig.Certificates = staticCerts
		return true
	}

	cacheDir := acmeCache
	if cacheDir == "" {
		cacheDir = config.GetACMEDir()
	}
	mgr := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(acmeHosts...),
		Cache:      autocert.DirCache(cacheDir),
		Email:      acmeEmail,
	}
	lg.Infof("ACME auto-TLS enabled for %v (cache %s)", acmeHosts, cacheDir)

	// TLS-ALPN-01 needs the acme protocol advertised in ALPN.
	server.TLSConfig.NextProtos = []string{"h2", "http/1.1", acme.ALPNProto}

	// Prefer a matching static certificate (for non-ACME hosts sharing the
	// port), otherwise let the ACME manager serve/obtain one.
	certs := staticCerts
	server.TLSConfig.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		for i := range certs {
			if err := hello.SupportsCertificate(&certs[i]); err == nil {
				return &certs[i], nil
			}
		}
		return mgr.GetCertificate(hello)
	}
	return true
}
