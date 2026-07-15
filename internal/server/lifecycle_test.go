package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandlerReady(t *testing.T) {
	SetReady(true)
	defer SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/up", nil)
	rec := httptest.NewRecorder()

	HealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHealthHandlerDraining(t *testing.T) {
	SetReady(false)
	defer SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/up", nil)
	rec := httptest.NewRecorder()

	HealthHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestWithHealthCheckBypassesHandler(t *testing.T) {
	SetReady(true)
	defer SetReady(true)

	called := false
	handler := withHealthCheck(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/up", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("expected health check to bypass handler")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}
