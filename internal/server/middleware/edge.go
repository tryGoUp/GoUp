package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/mirkobrombin/goup/internal/config"
)

// parseCIDRs turns a list of CIDRs or bare IPs into networks. A bare IP becomes
// a host route (/32 or /128).
func parseCIDRs(list []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			if ip := net.ParseIP(item); ip != nil {
				if ip.To4() != nil {
					item += "/32"
				} else {
					item += "/128"
				}
			}
		}
		if _, n, err := net.ParseCIDR(item); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IPFilterMiddleware allows or denies requests by client IP. If allow is
// non-empty, only clients within it may connect; deny always wins. The client
// IP is taken from RemoteAddr (the real peer), never from spoofable headers.
func IPFilterMiddleware(allow, deny []string) MiddlewareFunc {
	allowNets := parseCIDRs(allow)
	denyNets := parseCIDRs(deny)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip := net.ParseIP(host)
			if ip == nil {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			if len(denyNets) > 0 && ipInNets(ip, denyNets) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			if len(allowNets) > 0 && !ipInNets(ip, allowNets) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ForceHTTPSMiddleware redirects plain-HTTP requests to their HTTPS equivalent
// (301). Put a site's HTTP listener behind this and its HTTPS listener behind
// the real handler.
func ForceHTTPSMiddleware() MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS == nil {
				host := r.Host
				if h, _, err := net.SplitHostPort(host); err == nil {
					host = h
				}
				http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusMovedPermanently)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimitMiddleware caps the request body size to guard against memory
// exhaustion. max <= 0 means unlimited.
func BodyLimitMiddleware(max int64) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if max > 0 && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware sets HSTS (only over TLS) and a set of conservative
// security headers when enabled for the site.
func SecurityHeadersMiddleware(hsts bool, hstsMaxAge int, extra bool) MiddlewareFunc {
	if hstsMaxAge <= 0 {
		hstsMaxAge = 31536000
	}
	hstsValue := "max-age=" + strconv.Itoa(hstsMaxAge) + "; includeSubDomains"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if hsts && r.TLS != nil {
				h.Set("Strict-Transport-Security", hstsValue)
			}
			if extra {
				h.Set("X-Content-Type-Options", "nosniff")
				if h.Get("X-Frame-Options") == "" {
					h.Set("X-Frame-Options", "SAMEORIGIN")
				}
				if h.Get("Referrer-Policy") == "" {
					h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware adds CORS headers and short-circuits preflight OPTIONS
// requests according to the site's configuration.
func CORSMiddleware(cfg *config.CORSConfig) MiddlewareFunc {
	allowAll := false
	origins := make(map[string]bool)
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
		}
		origins[o] = true
	}
	methods := "GET, POST, PUT, DELETE, PATCH, OPTIONS"
	if len(cfg.AllowedMethods) > 0 {
		methods = strings.Join(cfg.AllowedMethods, ", ")
	}
	headers := "Content-Type, Authorization"
	if len(cfg.AllowedHeaders) > 0 {
		headers = strings.Join(cfg.AllowedHeaders, ", ")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowAll || origins[origin]) {
				h := w.Header()
				if allowAll && !cfg.AllowCredentials {
					h.Set("Access-Control-Allow-Origin", "*")
				} else {
					h.Set("Access-Control-Allow-Origin", origin)
					h.Add("Vary", "Origin")
				}
				if cfg.AllowCredentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
				if r.Method == http.MethodOptions {
					h.Set("Access-Control-Allow-Methods", methods)
					h.Set("Access-Control-Allow-Headers", headers)
					if cfg.MaxAge > 0 {
						h.Set("Access-Control-Max-Age", fmt.Sprintf("%d", cfg.MaxAge))
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
