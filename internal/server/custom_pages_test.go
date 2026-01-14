package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeStatic_CustomPages(t *testing.T) {
	rootDir := t.TempDir()

	// Test 404 - File Not Found
	t.Run("404 Custom Page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nonexistent.html", nil)
		w := httptest.NewRecorder()

		ServeStatic(w, req, rootDir)

		resp := w.Result()
		if resp.StatusCode != 404 {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}

		body := w.Body.String()
		if !strings.Contains(body, "GoUp") {
			t.Error("Expected body to contain 'GoUp'")
		}
		if !strings.Contains(body, "Page Not Found") {
			t.Error("Expected body to contain 'Page Not Found'")
		}
	})

	// Test Welcome Page - Missing Index
	t.Run("Welcome Page", func(t *testing.T) {
		// Ensure rootDir is empty/exists but has no index.html
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		ServeStatic(w, req, rootDir)

		resp := w.Result()
		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Welcome to GoUp") {
			t.Error("Expected body to contain 'Welcome to GoUp'")
		}
		if !strings.Contains(body, "<!DOCTYPE html>") {
			t.Error("Expected HTML response (template execution), got fallback text")
		}
	})

	// Test Index Existing - Should NOT show Welcome Page
	t.Run("Index Existing", func(t *testing.T) {
		indexContent := "<html>Index</html>"
		os.WriteFile(filepath.Join(rootDir, "index.html"), []byte(indexContent), 0644)

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		ServeStatic(w, req, rootDir)

		resp := w.Result()
		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body := w.Body.String()
		if body != indexContent {
			t.Errorf("Expected index content '%s', got '%s'", indexContent, body)
		}
	})
}
