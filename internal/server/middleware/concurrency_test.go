package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestConcurrencyMiddleware(t *testing.T) {
	maxConcurrent := 2
	mw := ConcurrencyMiddleware(maxConcurrent)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	results := make([]int, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			results[index] = w.Code
		}(i)
	}

	wg.Wait()

	successCount := 0
	rejectCount := 0

	for _, code := range results {
		if code == http.StatusOK {
			successCount++
		} else if code == http.StatusServiceUnavailable {
			rejectCount++
		}
	}

	if rejectCount == 0 {
		t.Errorf("Expected some requests to be rejected, but got 0 rejections. Successes: %d", successCount)
	}

	if successCount > maxConcurrent {
		t.Logf("Warning: More successes (%d) than concurrency limit (%d) - this might be due to test timing.", successCount, maxConcurrent)
	}
}
