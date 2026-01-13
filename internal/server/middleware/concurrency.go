package middleware

import (
	"net/http"
)

// ConcurrencyMiddleware limits the number of concurrent requests.
func ConcurrencyMiddleware(maxConcurrent int) MiddlewareFunc {
	// Semaphore channel to limit concurrent access
	sem := make(chan struct{}, maxConcurrent)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				// Acquired token
				defer func() { <-sem }() // Release token
				next.ServeHTTP(w, r)
			default:
				// Limit reached
				http.Error(w, "Service Unavailable (Max Concurrent Connections Reached)", http.StatusServiceUnavailable)
			}
		})
	}
}
