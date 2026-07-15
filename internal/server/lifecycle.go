package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultShutdownTimeout = 10 * time.Second

var (
	ready           atomic.Bool
	serverRegistry  []*http.Server
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

func ShutdownServers(timeout time.Duration) error {
	SetReady(false)

	serverRegistryM.Lock()
	servers := append([]*http.Server(nil), serverRegistry...)
	serverRegistryM.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, len(servers))

	for _, srv := range servers {
		wg.Add(1)
		go func(s *http.Server) {
			defer wg.Done()
			if err := s.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}(srv)
	}

	wg.Wait()
	close(errs)

	var shutdownErr error
	for err := range errs {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	return shutdownErr
}
