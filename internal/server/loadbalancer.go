package server

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/mirkobrombin/goup/internal/assets"
	"github.com/mirkobrombin/goup/internal/logger"
)

// lbFailKeyType is the context key used to signal a retryable transport failure
// from a backend's ErrorHandler back to the load balancer.
type lbFailKeyType struct{}

var lbFailKey = lbFailKeyType{}

// lbCooldown is how long a backend stays ejected after a failure before it is
// passively retried.
const lbCooldown = 30 * time.Second

type lbBackend struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
	down   atomic.Bool
	downAt atomic.Int64 // unix nanos when marked down
}

func (b *lbBackend) isDown() bool {
	if !b.down.Load() {
		return false
	}
	if time.Since(time.Unix(0, b.downAt.Load())) > lbCooldown {
		// Cooldown elapsed: give the backend another chance.
		b.down.Store(false)
		return false
	}
	return true
}

func (b *lbBackend) markDown() {
	b.downAt.Store(time.Now().UnixNano())
	b.down.Store(true)
}

// loadBalancer round-robins requests across healthy backends, ejecting a backend
// on a connection failure and retrying the next one, as long as nothing has been
// written to the client yet.
type loadBalancer struct {
	backends []*lbBackend
	counter  atomic.Uint64
	log      *logger.Logger
}

// trackWriter records whether any part of the response has been committed, so
// the balancer knows when it is still safe to retry another backend.
type trackWriter struct {
	http.ResponseWriter
	wrote bool
}

func (t *trackWriter) WriteHeader(code int) {
	t.wrote = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackWriter) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(b)
}

func newLoadBalancer(targets []string, log *logger.Logger) (*loadBalancer, error) {
	lb := &loadBalancer{log: log}
	for _, t := range targets {
		u, err := url.Parse(t)
		if err != nil {
			return nil, err
		}
		proxy := httputil.NewSingleHostReverseProxy(u)
		proxy.Transport = defaultTransport
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			if f, ok := r.Context().Value(lbFailKey).(*bool); ok {
				*f = true
				return // let the balancer retry another backend
			}
			assets.RenderErrorPage(w, http.StatusBadGateway, "Bad Gateway", "Unable to reach the backend server.")
		}
		lb.backends = append(lb.backends, &lbBackend{target: u, proxy: proxy})
	}
	return lb, nil
}

func (lb *loadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := len(lb.backends)
	start := int(lb.counter.Add(1) % uint64(n))

	for i := 0; i < n; i++ {
		b := lb.backends[(start+i)%n]
		if b.isDown() {
			continue
		}

		failed := false
		ctx := context.WithValue(r.Context(), lbFailKey, &failed)
		tw := &trackWriter{ResponseWriter: w}
		b.proxy.ServeHTTP(tw, r.WithContext(ctx))

		if !failed {
			return // success
		}
		b.markDown()
		lb.log.Errorf("Upstream %s failed, ejecting for %s", b.target, lbCooldown)
		if tw.wrote {
			// Response already partially sent; cannot safely retry.
			return
		}
	}

	// Every backend was down or failed.
	assets.RenderErrorPage(w, http.StatusBadGateway, "Bad Gateway", "No healthy backend available.")
}
