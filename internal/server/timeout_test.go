package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/server/middleware"
)

func TestCreateHandler_RequestTimeout(t *testing.T) {
	tests := []struct {
		name           string
		requestTimeout int
		handlerDelay   time.Duration
		expectedStatus int
	}{
		{
			name:           "Timeout Enabled - Request Times Out",
			requestTimeout: 1, // 1 second timeout
			handlerDelay:   2 * time.Second,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "Timeout Disabled - Request Completes",
			requestTimeout: -1, // Timeout disabled
			handlerDelay:   2 * time.Second,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Timeout Disabled (Zero-ish) - Request Completes",
			requestTimeout: -5, // Negative value should result in 0 duration in server config, effecitvely disabled
			handlerDelay:   2 * time.Second,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := config.SiteConfig{
				Domain:         "example.com",
				RequestTimeout: tt.requestTimeout,
				RootDirectory:  ".", // Dummy
			}

			testLogger, _ := logger.NewLogger("test", nil)
			testLogger.SetOutput(httptest.NewRecorder())

			testLogger.SetOutput(httptest.NewRecorder())

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(tt.handlerDelay)
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			conf.ProxyPass = backend.URL

			handler, err := createHandler(conf, testLogger, "test", &middleware.MiddlewareManager{})
			if err != nil {
				t.Fatalf("createHandler failed: %v", err)
			}

			req := httptest.NewRequest("GET", "http://example.com/", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}
