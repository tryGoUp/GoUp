package api

import (
	"fmt"
	"net/http"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/middleware"
)

// StartAPIServer starts the GoUp API server.
func StartAPIServer() {
	if config.GlobalConf == nil || !config.GlobalConf.EnableAPI {
		return
	}

	router := SetupRoutes()
	port := config.GlobalConf.APIPort

	go func() {
		fmt.Printf("[API] Listening on :%d\n", port)

		var handler http.Handler = router
		handler = middleware.TokenAuthMiddleware(handler)

		if err := http.ListenAndServe(fmt.Sprintf(":%d", port), handler); err != nil {
			fmt.Printf("[API] Error: %v\n", err)
		}
	}()
}
