package api

import (
	"fmt"
	"net/http"

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

	router := SetupRoutes()
	port := conf.APIPort
	var handler http.Handler = router
	handler = middleware.TokenAuthMiddleware(handler)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	go func() {
		fmt.Printf("[API] Listening on :%d\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[API] Error: %v\n", err)
		}
	}()

	return srv
}
