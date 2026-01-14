package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipMiddleware_Compression(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello World Repeat " + strings.Repeat("A", 1000)))
	})

	mw := GzipMiddleware(handler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	resp := w.Result()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("Expected Content-Encoding: gzip")
	}

	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	body, _ := io.ReadAll(gr)
	if !strings.Contains(string(body), "Hello World") {
		t.Errorf("Decoded content mismatch")
	}
}

func TestGzipMiddleware_Passthrough_Image(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-data"))
	})

	mw := GzipMiddleware(handler)

	req := httptest.NewRequest("GET", "/img.png", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	resp := w.Result()

	if resp.Header.Get("Content-Encoding") == "gzip" {
		t.Errorf("Should NOT compress image/png")
	}
}

func TestGzipMiddleware_Smart_Passthrough_PreCompressed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "gzip")
		w.Write([]byte("already-compressed-data"))
	})

	mw := GzipMiddleware(handler)

	req := httptest.NewRequest("GET", "/style.css", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	resp := w.Result()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("Expected Content-Encoding: gzip")
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "already-compressed-data" {
		t.Errorf("Middleware double-compressed or modified the body!")
	}
}
