package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mirkobrombin/goup/internal/logger"
)

var benchPayloadHTML = bytes.Repeat([]byte("<p>goup benchmark content</p>\n"), 200)
var benchPayloadBin = bytes.Repeat([]byte{0x42}, 6000)

func benchGzip(b *testing.B, contentType string, payload []byte, acceptEncoding string) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(payload)
	})
	h := GzipMiddleware(inner)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/bench", nil)
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func BenchmarkGzip_CompressibleHTML(b *testing.B) {
	benchGzip(b, "text/html", benchPayloadHTML, "gzip")
}

func BenchmarkGzip_NonCompressibleBinary(b *testing.B) {
	benchGzip(b, "application/octet-stream", benchPayloadBin, "gzip")
}

func BenchmarkGzip_ClientWithoutGzip(b *testing.B) {
	benchGzip(b, "text/html", benchPayloadHTML, "")
}

func BenchmarkLoggingMiddleware(b *testing.B) {
	l, err := logger.NewLogger("bench", nil)
	if err != nil {
		b.Fatal(err)
	}
	l.SetOutput(&bytes.Buffer{})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := LoggingMiddleware(l, "bench.local", "bench")(inner)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/bench?x=1", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}
