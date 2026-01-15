package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/mirkobrombin/goup/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// BasicAuthMiddleware enforces Basic Authentication if credentials are configured.
func BasicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is not configured, skip
		if config.GlobalConf == nil || config.GlobalConf.Account.Username == "" || config.GlobalConf.Account.PasswordHash == "" {
			next.ServeHTTP(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(config.GlobalConf.Account.Username)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoUp Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		err := bcrypt.CompareHashAndPassword([]byte(config.GlobalConf.Account.PasswordHash), []byte(pass))
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoUp Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// TokenAuthMiddleware enforces Token Authentication if a token is configured.
func TokenAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If token is not configured, skip
		if config.GlobalConf == nil || config.GlobalConf.Account.APIToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get("X-API-Token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(config.GlobalConf.Account.APIToken)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
