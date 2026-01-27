package middleware

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/tui"
)

// LoggingMiddleware logs HTTP requests.
func LoggingMiddleware(l *logger.Logger, domain string, identifier string) MiddlewareFunc {
	// sync.Pool for responseWriter to reduce allocation (Operation "31")
	rwPool := sync.Pool{
		New: func() any {
			return &responseWriter{}
		},
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Get ResponseWriter from pool
			rw := rwPool.Get().(*responseWriter)
			rw.ResponseWriter = w
			rw.statusCode = http.StatusOK

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			// Extract the real IP address if behind proxies
			remoteAddr := r.RemoteAddr
			if ip := r.Header.Get("X-Real-IP"); ip != "" {
				remoteAddr = ip
			} else if ips := r.Header.Get("X-Forwarded-For"); ips != "" {
				remoteAddr = ips
			}

			// Use Async Logging if initialized
			if asyncLog := GetAsyncLogger(); asyncLog != nil {
				entry := asyncLog.GetEntry()
				entry.Logger = l
				entry.Message = "Handled request"
				entry.Identifier = identifier
				entry.Fields["method"] = r.Method
				entry.Fields["url"] = r.URL.String()
				entry.Fields["remote_addr"] = remoteAddr
				entry.Fields["status_code"] = rw.statusCode
				entry.Fields["duration_sec"] = duration.Seconds()
				entry.Fields["domain"] = domain

				asyncLog.Log(entry)
			} else {
				// Fallback to sync logging (creates allocations)
				fields := logger.Fields{
					"method":       r.Method,
					"url":          r.URL.String(),
					"remote_addr":  remoteAddr,
					"status_code":  rw.statusCode,
					"duration_sec": duration.Seconds(),
					"domain":       domain,
				}
				l.WithFields(fields).Info("Handled request")
				if tui.IsEnabled() {
					tui.UpdateLog(identifier, fields)
				}
			}

			rw.ResponseWriter = nil
			rwPool.Put(rw)
		})
	}
}

// TimeoutMiddleware applies a timeout to HTTP requests.
func TimeoutMiddleware(timeout time.Duration) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, timeout, "Request timed out")
	}
}

// BenchmarkMiddleware logs the duration of HTTP requests.
func BenchmarkMiddleware() MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			next.ServeHTTP(w, r)

			duration := time.Since(start)
			fmt.Printf("\033[33;40m⏲ Benchmark: %s %s completed in %s\033[0m\n",
				r.Method, r.URL.Path, formatDuration(duration))
		})
	}
}

// formatDuration formats a time.Duration to a human-readable string.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.3fms", float64(d.Microseconds())/1000)
	default:
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader sets the HTTP status code.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher.
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// ReadFrom implements io.ReaderFrom.
func (rw *responseWriter) ReadFrom(r io.Reader) (n int64, err error) {
	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(rw.ResponseWriter, r)
}

// Push implements http.Pusher.
func (rw *responseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := rw.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
