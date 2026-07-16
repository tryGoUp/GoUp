package plugins

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/mirkobrombin/goup/internal/logger"
)

// upstreamTransport is shared by all plugin reverse proxies so connections to
// application backends (Node, Python, Docker) are pooled and reused instead of
// being re-dialed per request.
var upstreamTransport = &http.Transport{
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
	// Bound the wait for a backend's response headers so a hung app process
	// (Node, Python, Docker container) cannot hold the proxy goroutine open.
	ResponseHeaderTimeout: 60 * time.Second,
}

var upstreamProxies sync.Map // target URL -> *httputil.ReverseProxy

// upstreamBufferPool recycles the copy buffers used by the reverse proxies so
// small responses do not pay a fresh 32KB allocation per request.
type upstreamBufferPool struct{ pool sync.Pool }

func (p *upstreamBufferPool) Get() []byte  { return p.pool.Get().([]byte) }
func (p *upstreamBufferPool) Put(b []byte) { p.pool.Put(b) }

var sharedBufferPool = &upstreamBufferPool{
	pool: sync.Pool{
		New: func() any { return make([]byte, 32*1024) },
	},
}

// upstreamProxy returns a shared streaming reverse proxy for the given
// backend base URL (e.g. "http://localhost:3000"). Request and response
// bodies are streamed end-to-end, never buffered in memory.
func upstreamProxy(target string, l *logger.Logger) (*httputil.ReverseProxy, error) {
	if cached, ok := upstreamProxies.Load(target); ok {
		return cached.(*httputil.ReverseProxy), nil
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	rp := httputil.NewSingleHostReverseProxy(parsed)
	rp.Transport = upstreamTransport
	rp.BufferPool = sharedBufferPool
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if l != nil {
			l.Errorf("Proxy error for %s -> %s: %v", r.URL.Path, target, err)
		}
		http.Error(w, "Backend unavailable", http.StatusBadGateway)
	}

	actual, _ := upstreamProxies.LoadOrStore(target, rp)
	return actual.(*httputil.ReverseProxy), nil
}
