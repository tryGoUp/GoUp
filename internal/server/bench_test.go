package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// benchStaticRoot builds a document root with files of different sizes and
// pre-compressed sidecars, mirroring the benchmark matrix from the
// performance plan.
func benchStaticRoot(b *testing.B) string {
	b.Helper()
	root := b.TempDir()

	writeFile := func(name string, size int) {
		data := bytes.Repeat([]byte("goup data "), size/10+1)[:size]
		if err := os.WriteFile(filepath.Join(root, name), data, 0644); err != nil {
			b.Fatal(err)
		}
	}
	writeFile("tiny.txt", 128)
	writeFile("small.html", 4*1024)
	writeFile("medium.js", 64*1024)
	writeFile("large.bin", 1024*1024)

	// Pre-compressed sidecar for the medium file.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(bytes.Repeat([]byte("goup data "), 64*1024/10+1)[:64*1024])
	gz.Close()
	if err := os.WriteFile(filepath.Join(root, "medium.js.gz"), buf.Bytes(), 0644); err != nil {
		b.Fatal(err)
	}

	os.Mkdir(filepath.Join(root, "listing"), 0755)
	for i := 0; i < 20; i++ {
		writeFile(filepath.Join("listing", "entry-"+string(rune('a'+i))+".txt"), 64)
	}
	return root
}

func benchServeStatic(b *testing.B, path, acceptEncoding string) {
	root := benchStaticRoot(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", path, nil)
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		w := httptest.NewRecorder()
		ServeStatic(w, req, root)
		if w.Code != http.StatusOK {
			b.Fatalf("status %d for %s", w.Code, path)
		}
	}
}

func BenchmarkServeStatic_Tiny(b *testing.B) { benchServeStatic(b, "/tiny.txt", "") }
func BenchmarkServeStatic_4KB(b *testing.B)  { benchServeStatic(b, "/small.html", "") }
func BenchmarkServeStatic_64KB(b *testing.B) { benchServeStatic(b, "/medium.js", "") }
func BenchmarkServeStatic_1MB(b *testing.B)  { benchServeStatic(b, "/large.bin", "") }
func BenchmarkServeStatic_Precompressed(b *testing.B) {
	benchServeStatic(b, "/medium.js", "gzip")
}
func BenchmarkServeStatic_Listing(b *testing.B) {
	benchServeStatic(b, "/listing/", "")
}

func BenchmarkAddCustomHeaders(b *testing.B) {
	headers := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"Permissions-Policy":     "geolocation=()",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		addCustomHeaders(w, headers)
	}
}
