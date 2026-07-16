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
		config.GlobalConfMu.RLock()
		conf := config.GlobalConf
		config.GlobalConfMu.RUnlock()

		// Fail closed: if credentials are not configured, deny rather than
		// serving the protected surface unauthenticated.
		if conf == nil || conf.Account.Username == "" || conf.Account.PasswordHash == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoUp Dashboard"`)
			http.Error(w, "Unauthorized: authentication is not configured", http.StatusUnauthorized)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(conf.Account.Username)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoUp Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		err := bcrypt.CompareHashAndPassword([]byte(conf.Account.PasswordHash), []byte(pass))
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
		config.GlobalConfMu.RLock()
		conf := config.GlobalConf
		config.GlobalConfMu.RUnlock()

		// Fail closed: if no token is configured, deny rather than serving the
		// admin API unauthenticated.
		if conf == nil || conf.Account.APIToken == "" {
			http.Error(w, "Unauthorized: authentication is not configured", http.StatusUnauthorized)
			return
		}

		token := r.Header.Get("X-API-Token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(conf.Account.APIToken)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
