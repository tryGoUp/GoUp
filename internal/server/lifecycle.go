package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultShutdownTimeout = 10 * time.Second

var (
	ready           atomic.Bool
	serverRegistry  []*http.Server
	closerRegistry  []io.Closer
	serverRegistryM sync.Mutex
)

func init() {
	ready.Store(true)
}

func SetReady(v bool) {
	ready.Store(v)
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, "draining")
		return
	}

	fmt.Fprintln(w, "ok")
}

func withHealthCheck(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/up" {
			HealthHandler(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func registerServer(server *http.Server) {
	serverRegistryM.Lock()
	defer serverRegistryM.Unlock()
	serverRegistry = append(serverRegistry, server)
}

// registerCloser registers a resource (HTTP/3 server, DNS server, ...) that is
// not an *http.Server but must still be closed on shutdown.
func registerCloser(c io.Closer) {
	serverRegistryM.Lock()
	defer serverRegistryM.Unlock()
	closerRegistry = append(closerRegistry, c)
}

func ShutdownServers(timeout time.Duration) error {
	SetReady(false)

	serverRegistryM.Lock()
	servers := append([]*http.Server(nil), serverRegistry...)
	closers := append([]io.Closer(nil), closerRegistry...)
	serverRegistryM.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, len(servers)+len(closers))

	for _, srv := range servers {
		wg.Add(1)
		go func(s *http.Server) {
			defer wg.Done()
			if err := s.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}(srv)
	}

	// HTTP/3 and DNS servers expose Close, not graceful Shutdown; close them in
	// parallel so QUIC listeners and DNS sockets are released on shutdown.
	for _, c := range closers {
		wg.Add(1)
		go func(cl io.Closer) {
			defer wg.Done()
			if err := cl.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}(c)
	}

	wg.Wait()
	close(errs)

	var shutdownErr error
	for err := range errs {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	return shutdownErr
}
