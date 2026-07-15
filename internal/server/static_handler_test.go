package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeStatic_DirectoryTrailingSlashRedirect(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "goup_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	target := filepath.Join(tempDir, "target")
	os.Mkdir(target, 0755)
	os.WriteFile(filepath.Join(target, "inner.txt"), []byte("inner"), 0644)
	if err := os.Symlink(target, filepath.Join(tempDir, "link")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		location       string
	}{
		{"Dir without slash", "/target", http.StatusMovedPermanently, "/target/"},
		{"Symlinked dir without slash", "/link", http.StatusMovedPermanently, "/link/"},
		{"Dir without slash keeps query", "/target?a=1", http.StatusMovedPermanently, "/target/?a=1"},
		{"Dir with slash serves listing", "/target/", http.StatusOK, ""},
		{"Symlinked dir with slash serves listing", "/link/", http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			ServeStatic(w, req, tempDir)

			resp := w.Result()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
			if tt.location != "" {
				if loc := resp.Header.Get("Location"); loc != tt.location {
					t.Errorf("expected Location %q, got %q", tt.location, loc)
				}
			}
			if tt.expectedStatus == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				if !strings.Contains(string(body), "inner.txt") {
					t.Errorf("expected listing to contain inner.txt, body: %s", string(body))
				}
			}
		})
	}
}

func TestServeStatic_ContentNegotiation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "goup_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	os.Mkdir(filepath.Join(tempDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tempDir, "file.txt"), []byte("content"), 0644)

	tests := []struct {
		name           string
		path           string
		accept         string
		expectedStatus int
		contains       string
		isHTML         bool
	}{
		{
			name:           "CLI Listing",
			path:           "/",
			accept:         "*/*",
			expectedStatus: http.StatusOK,
			contains:       "file.txt",
			isHTML:         false,
		},
		{
			name:           "Browser Listing",
			path:           "/",
			accept:         "text/html,application/xhtml+xml",
			expectedStatus: http.StatusOK,
			contains:       "Index of",
			isHTML:         true,
		},
		{
			name:           "CLI 404",
			path:           "/missing",
			accept:         "*/*",
			expectedStatus: http.StatusNotFound,
			contains:       "404 Not Found",
			isHTML:         false,
		},
		{
			name:           "Browser 404",
			path:           "/missing",
			accept:         "text/html",
			expectedStatus: http.StatusNotFound,
			contains:       "Page Not Found",
			isHTML:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			w := httptest.NewRecorder()

			ServeStatic(w, req, tempDir)

			resp := w.Result()
			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			contentType := resp.Header.Get("Content-Type")
			if tt.isHTML && !strings.Contains(contentType, "text/html") {
				t.Errorf("expected HTML content-type, got %s", contentType)
			}
			if !tt.isHTML && !strings.Contains(contentType, "text/plain") {
				t.Errorf("expected plain text content-type, got %s", contentType)
			}

			if !strings.Contains(string(body), tt.contains) {
				t.Errorf("expected body to contain %q, but it didn't. Body: %s", tt.contains, string(body))
			}
		})
	}
}

func TestStaticLocalPath_BackslashTraversal(t *testing.T) {
	root := t.TempDir()

	cleanPath, fullPath, err := staticLocalPath(root, `\..\secret.txt`)
	if err != nil {
		t.Fatal(err)
	}
	if cleanPath != "/secret.txt" {
		t.Fatalf("expected clean path /secret.txt, got %s", cleanPath)
	}
	expected := filepath.Join(root, "secret.txt")
	if fullPath != expected {
		t.Fatalf("expected local path %s, got %s", expected, fullPath)
	}
}
