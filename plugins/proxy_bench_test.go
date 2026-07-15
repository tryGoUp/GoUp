package plugins

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mirkobrombin/goup/internal/logger"
)

// startBenchBackend returns a backend that echoes a fixed payload, standing in
// for a Node.js/Python application server.
func startBenchBackend(b *testing.B, payloadSize int) (*httptest.Server, string) {
	b.Helper()
	payload := bytes.Repeat([]byte("x"), payloadSize)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(payload)
	}))
	b.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		b.Fatal(err)
	}
	return srv, u.Port()
}

func benchNodeProxy(b *testing.B, respSize, bodySize int) {
	_, port := startBenchBackend(b, respSize)

	l, err := logger.NewLogger("bench", nil)
	if err != nil {
		b.Fatal(err)
	}
	l.SetOutput(&bytes.Buffer{})

	p := &NodeJSPlugin{}
	p.PluginLogger = l
	p.DomainLogger = l
	cfg := NodeJSPluginConfig{Enable: true, Port: port}

	body := strings.Repeat("y", bodySize)

	b.ReportAllocs()
	b.SetBytes(int64(respSize + bodySize))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var reqBody *strings.Reader
		if bodySize > 0 {
			reqBody = strings.NewReader(body)
		} else {
			reqBody = strings.NewReader("")
		}
		req := httptest.NewRequest("POST", "http://bench.local/api/echo", reqBody)
		w := httptest.NewRecorder()
		p.proxyToNode(w, req, cfg)
		if w.Code != http.StatusOK {
			b.Fatalf("status %d", w.Code)
		}
	}
}

func BenchmarkNodeProxy_Resp4KB(b *testing.B) { benchNodeProxy(b, 4*1024, 0) }
func BenchmarkNodeProxy_Resp1MB(b *testing.B) { benchNodeProxy(b, 1024*1024, 0) }
func BenchmarkNodeProxy_Post1MB(b *testing.B) { benchNodeProxy(b, 1024, 1024*1024) }
