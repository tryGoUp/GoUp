package server

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeStatic_PreCompressed(t *testing.T) {
	rootDir := t.TempDir()

	content := "Hello World"
	filePath := filepath.Join(rootDir, "test.txt")
	os.WriteFile(filePath, []byte(content), 0644)

	gzPath := filePath + ".gz"
	gzFile, _ := os.Create(gzPath)
	gw := gzip.NewWriter(gzFile)
	gw.Write([]byte(content))
	gw.Close()
	gzFile.Close()

	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	ServeStatic(w, req, rootDir)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("Expected Content-Encoding: gzip, got %s", resp.Header.Get("Content-Encoding"))
	}
	if resp.Header.Get("Vary") != "Accept-Encoding" {
		t.Errorf("Expected Vary: Accept-Encoding")
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) == content {
		t.Errorf("Expected compressed body, got plain text")
	}

	req2 := httptest.NewRequest("GET", "/test.txt", nil)
	w2 := httptest.NewRecorder()

	ServeStatic(w2, req2, rootDir)
	resp2 := w2.Result()

	if resp2.Header.Get("Content-Encoding") != "" {
		t.Errorf("Expected no Content-Encoding, got %s", resp2.Header.Get("Content-Encoding"))
	}
	body2, _ := io.ReadAll(resp2.Body)
	if string(body2) != content {
		t.Errorf("Expected plain text body, got %s", string(body2))
	}
}

func TestServeStatic_ETag(t *testing.T) {
	rootDir := t.TempDir()
	filePath := filepath.Join(rootDir, "etag.txt")
	os.WriteFile(filePath, []byte("cache me"), 0644)

	req := httptest.NewRequest("GET", "/etag.txt", nil)
	w := httptest.NewRecorder()

	ServeStatic(w, req, rootDir)

	etag := w.Result().Header.Get("ETag")
	if etag == "" {
		t.Fatal("Expected ETag header")
	}

	if !strings.HasPrefix(etag, "\"") || !strings.HasSuffix(etag, "\"") {
		t.Errorf("Expected quoted ETag, got %s", etag)
	}
}
