package middleware

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Compressible Content Types
var compressibleTypes = map[string]bool{
	"text/html":                true,
	"text/css":                 true,
	"text/plain":               true,
	"text/javascript":          true,
	"application/javascript":   true,
	"application/x-javascript": true,
	"application/json":         true,
	"application/xml":          true,
	"text/xml":                 true,
	"image/svg+xml":            true,
}

// gzipWriterPool for reusing gzip writers
var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(b))
		}
		w.WriteHeader(http.StatusOK)
	}
	return w.Writer.Write(b)
}

// GzipMiddleware compresses the response if the client supports it and the content is compressible.
// Critical: It skips compression if "Content-Encoding" is already set (e.g. by Smart Static Handler serving .gz).
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("Sec-WebSocket-Key") != "" {
			next.ServeHTTP(w, r)
			return
		}

		// Optimized Wrapper
		gw := &smartGzipWriter{
			ResponseWriter: w,
			req:            r,
		}

		defer gw.Close()

		next.ServeHTTP(gw, r)
	})
}

// smartGzipWriter determines at the last moment whether to compress or not.
type smartGzipWriter struct {
	http.ResponseWriter
	req            *http.Request
	gzipWriter     *gzip.Writer
	shouldCompress bool
	checked        bool
	status         int
}

func (w *smartGzipWriter) WriteHeader(status int) {
	if w.checked {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.checkCompression()

	if w.shouldCompress {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *smartGzipWriter) Write(b []byte) (int, error) {
	if !w.checked {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(b))
		}
		w.WriteHeader(http.StatusOK)
	}

	if w.shouldCompress {
		return w.gzipWriter.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *smartGzipWriter) checkCompression() {
	if w.checked {
		return
	}
	w.checked = true

	if w.Header().Get("Content-Encoding") != "" {
		w.shouldCompress = false
		return
	}

	ct := w.Header().Get("Content-Type")
	idx := strings.Index(ct, ";")
	if idx != -1 {
		ct = ct[:idx]
	}
	if !compressibleTypes[ct] {
		w.shouldCompress = false
		return
	}

	// Init Gzip
	gz := gzipWriterPool.Get().(*gzip.Writer)
	gz.Reset(w.ResponseWriter)
	w.gzipWriter = gz
	w.shouldCompress = true
}

func (w *smartGzipWriter) Close() {
	if w.shouldCompress && w.gzipWriter != nil {
		w.gzipWriter.Close()
		gzipWriterPool.Put(w.gzipWriter)
	}
}

// ReadFrom keeps the underlying ResponseWriter's fast path (sendfile) alive
// when no compression happens; http.ServeContent uses io.Copy, which probes
// for io.ReaderFrom on the writer it is handed.
func (w *smartGzipWriter) ReadFrom(src io.Reader) (int64, error) {
	if !w.checked {
		// Headers were not written yet (raw handlers): decide now with the
		// headers set so far.
		w.WriteHeader(http.StatusOK)
	}
	if w.shouldCompress {
		return io.Copy(w.gzipWriter, src)
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(w.ResponseWriter, src)
}

// Flush forwards streaming flushes (SSE, chunked responses).
func (w *smartGzipWriter) Flush() {
	if w.shouldCompress && w.gzipWriter != nil {
		w.gzipWriter.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack lets WebSocket and other upgraders take over the connection.
func (w *smartGzipWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push forwards HTTP/2 server pushes.
func (w *smartGzipWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}
