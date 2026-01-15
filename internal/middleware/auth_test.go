package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mirkobrombin/goup/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestBasicAuthMiddleware(t *testing.T) {
	// Setup
	password := "secret"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	config.GlobalConf = &config.GlobalConfig{
		Account: config.AccountConfig{
			Username:     "admin",
			PasswordHash: string(hash),
		},
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := BasicAuthMiddleware(nextHandler)

	tests := []struct {
		name           string
		user           string
		pass           string
		expectedStatus int
	}{
		{"ValidCredentials", "admin", "secret", http.StatusOK},
		{"InvalidUsername", "wrong", "secret", http.StatusUnauthorized},
		{"InvalidPassword", "admin", "wrong", http.StatusUnauthorized},
		{"NoCredentials", "", "", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.user != "" {
				req.SetBasicAuth(tt.user, tt.pass)
			}
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %v, got %v", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestTokenAuthMiddleware(t *testing.T) {
	// Setup
	token := "my-secret-token"
	config.GlobalConf = &config.GlobalConfig{
		Account: config.AccountConfig{
			APIToken: token,
		},
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := TokenAuthMiddleware(nextHandler)

	tests := []struct {
		name           string
		headers        map[string]string
		expectedStatus int
	}{
		{"ValidTokenHeader", map[string]string{"X-API-Token": token}, http.StatusOK},
		{"ValidBearerToken", map[string]string{"Authorization": "Bearer " + token}, http.StatusOK},
		{"InvalidToken", map[string]string{"X-API-Token": "wrong"}, http.StatusUnauthorized},
		{"NoToken", map[string]string{}, http.StatusOK},
	}

	tests = []struct {
		name           string
		headers        map[string]string
		expectedStatus int
	}{
		{"ValidTokenHeader", map[string]string{"X-API-Token": token}, http.StatusOK},
		{"ValidBearerToken", map[string]string{"Authorization": "Bearer " + token}, http.StatusOK},
		{"InvalidToken", map[string]string{"X-API-Token": "wrong"}, http.StatusUnauthorized},
		{"NoToken", map[string]string{}, http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %v, got %v", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestAuthSkippedIfUnconfigured(t *testing.T) {
	// Setup: Empty config
	config.GlobalConf = &config.GlobalConfig{}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("BasicAuthSkipped", func(t *testing.T) {
		middleware := BasicAuthMiddleware(nextHandler)
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected status %v, got %v", http.StatusOK, rec.Code)
		}
	})

	t.Run("TokenAuthSkipped", func(t *testing.T) {
		middleware := TokenAuthMiddleware(nextHandler)
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected status %v, got %v", http.StatusOK, rec.Code)
		}
	})
}
