package api

import (
	"fmt"
	"net/http"

	"github.com/mirkobrombin/goup/internal/config"
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

		if err := http.ListenAndServe(fmt.Sprintf(":%d", port), router); err != nil {
			fmt.Printf("[API] Error: %v\n", err)
		}
	}()
}
