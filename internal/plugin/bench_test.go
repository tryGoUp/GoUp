package plugin

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
)

// noopPlugin mimics a registered plugin that does not intercept requests,
// which is the common case for every plugin not matching the current site.
type noopPlugin struct{ name string }

func (p *noopPlugin) Name() string  { return p.name }
func (p *noopPlugin) OnInit() error { return nil }
func (p *noopPlugin) OnInitForSite(conf config.SiteConfig, l *logger.Logger) error {
	return nil
}
func (p *noopPlugin) BeforeRequest(r *http.Request) {}
func (p *noopPlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	return false
}
func (p *noopPlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}
func (p *noopPlugin) OnExit() error                                       { return nil }

// BenchmarkPluginMiddleware measures the per-request dispatch cost with the
// same number of built-in plugins the real server registers.
func BenchmarkPluginMiddleware(b *testing.B) {
	pm := NewPluginManager()
	for i := 0; i < 6; i++ {
		pm.Register(&noopPlugin{name: "bench-plugin-" + strconv.Itoa(i)})
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := PluginMiddleware(pm)(inner)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/bench", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func BenchmarkPluginMiddleware_Parallel(b *testing.B) {
	pm := NewPluginManager()
	for i := 0; i < 6; i++ {
		pm.Register(&noopPlugin{name: "bench-plugin-" + strconv.Itoa(i)})
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := PluginMiddleware(pm)(inner)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/bench", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
		}
	})
}
