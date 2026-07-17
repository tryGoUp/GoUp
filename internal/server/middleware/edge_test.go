package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestIPFilterMiddleware(t *testing.T) {
	// Deny loopback.
	h := IPFilterMiddleware(nil, []string{"127.0.0.0/8"})(okHandler())
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("denied IP: expected 403, got %d", rec.Code)
	}

	// Allowlist that does not include the client.
	h = IPFilterMiddleware([]string{"10.0.0.0/8"}, nil)(okHandler())
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-allowlisted IP: expected 403, got %d", rec.Code)
	}

	// Allowlist that includes the client.
	h = IPFilterMiddleware([]string{"127.0.0.1"}, nil)(okHandler())
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("allowlisted IP: expected 200, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	h := RateLimitMiddleware(1, 2)(okHandler())
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.5:9999"

	codes := make([]int, 4)
	for i := range codes {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes[i] = rec.Code
	}
	// Burst of 2 allowed, then limited.
	if codes[0] != 200 || codes[1] != 200 {
		t.Errorf("first two requests should pass, got %v", codes)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Errorf("third request should be 429, got %v", codes)
	}
}

func TestBodyLimitMiddleware(t *testing.T) {
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n := 0
		for {
			m, err := r.Body.Read(buf)
			n += m
			if err != nil {
				if err.Error() == "http: request body too large" {
					http.Error(w, "too big", http.StatusRequestEntityTooLarge)
					return
				}
				break
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	h := BodyLimitMiddleware(10)(echo)
	req := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 100)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: expected 413, got %d", rec.Code)
	}
}
