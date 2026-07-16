package server

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mirkobrombin/goup/internal/assets"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/server/middleware"
)

// createHandler creates the HTTP handler for a site configuration.
func createHandler(conf config.SiteConfig, log *logger.Logger, identifier string, globalMwManager *middleware.MiddlewareManager) (http.Handler, error) {
	var handler http.Handler

	// Precompute the expose-headers value once per site instead of joining
	// header names on every request.
	exposeHeaders := joinHeaderNames(conf.CustomHeaders)

	if conf.ProxyPass != "" {
		// Set up reverse proxy handler if ProxyPass is set.
		proxy, err := getSharedReverseProxy(conf, log)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %v", err)
		}

		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addCustomHeaders(w, conf.CustomHeaders, exposeHeaders)
			proxy.ServeHTTP(w, r)
		})

	} else {
		// Static File Handler with custom design and directory listing
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addCustomHeaders(w, conf.CustomHeaders, exposeHeaders)
			ServeStatic(w, r, conf.RootDirectory)
		})
	}

	// Copy the global middleware manager for this site
	siteMwManager := globalMwManager.Copy()

	// Add Concurrency Middleware
	if conf.MaxConcurrentConnections > 0 {
		siteMwManager.Use(middleware.ConcurrencyMiddleware(conf.MaxConcurrentConnections))
	}

	// Add Gzip Middleware (Smart Compression)
	// Keeps pre-compressed files if they exist, compresses others on the fly.
	siteMwManager.Use(middleware.GzipMiddleware)

	// Add logging middleware last to ensure it wraps the entire request.
	// We default to true if the pointer is nil.
	if conf.EnableLogging == nil || *conf.EnableLogging {
		siteMwManager.Use(middleware.LoggingMiddleware(log, conf.Domain, identifier))
	}

	// Apply the final chain of middleware
	handler = siteMwManager.Apply(handler)

	return handler, nil
}

// joinHeaderNames builds the Access-Control-Expose-Headers value for a set of
// custom headers.
func joinHeaderNames(headers map[string]string) string {
	names := make([]string, 0, len(headers))
	for key := range headers {
		names = append(names, key)
	}
	return strings.Join(names, ", ")
}

// addCustomHeaders adds custom headers to the HTTP response.
func addCustomHeaders(w http.ResponseWriter, headers map[string]string, exposeHeaders string) {
	h := w.Header()
	for key, value := range headers {
		h.Set(key, value)
	}
	h.Set("Access-Control-Expose-Headers", exposeHeaders)
}

var (
	sharedProxyMap   = make(map[string]*httputil.ReverseProxy)
	sharedProxyMapMu sync.Mutex
	// defaultTransport mirrors http.DefaultTransport but with a larger idle
	// pool, so proxied sites reuse backend connections under load.
	defaultTransport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   128,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// Bound the time spent waiting for a backend to start responding, so a
		// hung upstream cannot pin a goroutine/connection indefinitely.
		ResponseHeaderTimeout: 60 * time.Second,
	}

	globalBytePool = newByteSlicePool(32 * 1024)
)

type byteSlicePool struct {
	pool sync.Pool
	size int
}

func newByteSlicePool(size int) *byteSlicePool {
	p := &byteSlicePool{size: size}
	p.pool.New = func() any { return make([]byte, size) }
	return p
}

func (b *byteSlicePool) Get() []byte {
	return b.pool.Get().([]byte)
}

func (b *byteSlicePool) Put(buf []byte) {
	// Recycle only buffers that match this pool's size, so a per-site pool with
	// a custom buffer_size_kb actually reuses its buffers instead of allocating
	// a fresh one on every request.
	if cap(buf) >= b.size {
		b.pool.Put(buf[:b.size])
	}
}

// getSharedReverseProxy returns a shared ReverseProxy for the given site configuration.
func getSharedReverseProxy(conf config.SiteConfig, log *logger.Logger) (*httputil.ReverseProxy, error) {
	sharedProxyMapMu.Lock()
	defer sharedProxyMapMu.Unlock()

	key := fmt.Sprintf("%s|%s|%d", conf.ProxyPass, conf.FlushInterval, conf.BufferSizeKB)

	if rp, ok := sharedProxyMap[key]; ok {
		return rp, nil
	}

	parsedURL, err := url.Parse(conf.ProxyPass)
	if err != nil {
		return nil, err
	}

	rp := httputil.NewSingleHostReverseProxy(parsedURL)
	rp.Transport = defaultTransport

	// Set custom error handler for the proxy
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Errorf("Proxy error for %s: %v", r.URL.Path, err)
		assets.RenderErrorPage(w, http.StatusBadGateway, "Bad Gateway", "Unable to reach the backend server.")
	}

	// Set FlushInterval
	if conf.FlushInterval != "" {
		if d, err := time.ParseDuration(conf.FlushInterval); err == nil {
			rp.FlushInterval = d
		}
	}

	// Set BufferPool with custom size if specified
	if conf.BufferSizeKB > 0 {
		rp.BufferPool = newByteSlicePool(conf.BufferSizeKB * 1024)
	} else {
		rp.BufferPool = globalBytePool
	}

	sharedProxyMap[key] = rp
	return rp, nil
}
