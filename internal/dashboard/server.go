package dashboard

import (
	"fmt"
	"net/http"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/middleware"
)

// StartDashboardServer starts a dedicated server for the dashboard.
func StartDashboardServer() *http.Server {
	config.GlobalConfMu.RLock()
	conf := config.GlobalConf
	config.GlobalConfMu.RUnlock()

	if conf == nil || conf.DashboardPort == 0 {
		return nil
	}

	// Fail closed: the dashboard proxies the admin API (and injects its token).
	// Refuse to start without Basic Auth credentials rather than exposing it
	// unauthenticated on the network.
	if conf.Account.Username == "" || conf.Account.PasswordHash == "" {
		fmt.Println("[Dashboard] Refusing to start: 'account.username' and " +
			"'account.password_hash' must be set to secure the dashboard.")
		return nil
	}

	port := conf.DashboardPort
	handler := Handler()
	handler = middleware.BasicAuthMiddleware(handler)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	go func() {
		fmt.Printf("[Dashboard] Listening on :%d\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[Dashboard] Error: %v\n", err)
		}
	}()

	return srv
}
