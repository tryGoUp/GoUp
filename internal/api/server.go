package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/middleware"
)

// StartAPIServer starts the GoUp API server.
func StartAPIServer() *http.Server {
	config.GlobalConfMu.RLock()
	conf := config.GlobalConf
	config.GlobalConfMu.RUnlock()

	if conf == nil || !conf.EnableAPI {
		return nil
	}

	// Fail closed: the API exposes full administrative control (create/delete
	// sites, rewrite the global config, restart). Refuse to start it without a
	// token instead of serving an unauthenticated admin surface on the network.
	if conf.Account.APIToken == "" {
		fmt.Println("[API] Refusing to start: 'account.api_token' is not set. " +
			"Set a token in the global config to enable the API.")
		return nil
	}

	router := SetupRoutes()
	port := conf.APIPort
	var handler http.Handler = router
	handler = middleware.TokenAuthMiddleware(handler)

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", conf.APIBind, port),
		Handler: handler,
		// Timeouts guard the (pre-auth) admin surface against slowloris.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		fmt.Printf("[API] Listening on :%d\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[API] Error: %v\n", err)
		}
	}()

	return srv
}
