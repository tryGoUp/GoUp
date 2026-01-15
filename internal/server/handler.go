package server

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mirkobrombin/goup/internal/assets"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/internal/server/middleware"
)

// createHandler creates the HTTP handler for a site configuration.
func createHandler(conf config.SiteConfig, log *logger.Logger, identifier string, globalMwManager *middleware.MiddlewareManager) (http.Handler, error) {
	var handler http.Handler

	if conf.ProxyPass != "" {
		// Set up reverse proxy handler if ProxyPass is set.
		proxy, err := getSharedReverseProxy(conf, log)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %v", err)
		}

		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addCustomHeaders(w, conf.CustomHeaders)
			proxy.ServeHTTP(w, r)
		})

	} else {
		// Serve static files from the root directory
		if conf.FileServerMode {
			// File Server Mode: use standard http.FileServer with directory listing
			fs := http.FileServer(http.Dir(conf.RootDirectory))
			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				addCustomHeaders(w, conf.CustomHeaders)
				fs.ServeHTTP(w, r)
			})
		} else {
			// Smart Static Handler with custom error pages
			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				addCustomHeaders(w, conf.CustomHeaders)
				ServeStatic(w, r, conf.RootDirectory)
			})
		}
	}

	// Copy the global middleware manager for this site
	siteMwManager := globalMwManager.Copy()

	// Initialize plugins for this site
	pluginManager := plugin.GetPluginManagerInstance()
	if err := pluginManager.InitPluginsForSite(conf, log); err != nil {
		return nil, fmt.Errorf("error initializing plugins for site %s: %v", conf.Domain, err)
	}

	// Add per-site middleware
	reqTimeout := conf.RequestTimeout
	if reqTimeout == 0 {
		reqTimeout = 60 // Default to 60 seconds
	}

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

// addCustomHeaders adds custom headers to the HTTP response.
func addCustomHeaders(w http.ResponseWriter, headers map[string]string) {
	for key, value := range headers {
		w.Header().Set(key, value)
	}

	exposeHeaders := make([]string, 0, len(headers))
	for key := range headers {
		exposeHeaders = append(exposeHeaders, key)
	}

	w.Header().Set("Access-Control-Expose-Headers", strings.Join(exposeHeaders, ", "))
}

var (
	sharedProxyMap   = make(map[string]*httputil.ReverseProxy)
	sharedProxyMapMu sync.Mutex
	defaultTransport = &http.Transport{}

	globalBytePool = &byteSlicePool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 32*1024)
			},
		},
	}
)

type byteSlicePool struct {
	pool sync.Pool
}

func (b *byteSlicePool) Get() []byte {
	return b.pool.Get().([]byte)
}

func (b *byteSlicePool) Put(buf []byte) {
	if cap(buf) == 32*1024 {
		b.pool.Put(buf[:32*1024])
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
		rp.BufferPool = &byteSlicePool{
			pool: sync.Pool{
				New: func() interface{} {
					return make([]byte, conf.BufferSizeKB*1024)
				},
			},
		}
	} else {
		rp.BufferPool = globalBytePool
	}

	sharedProxyMap[key] = rp
	return rp, nil
}
