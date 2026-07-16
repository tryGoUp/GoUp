//go:build !dns_only
// +build !dns_only

package server

import (
	"fmt"
	"sync"

	"github.com/mirkobrombin/goup/internal/api"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/dashboard"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/internal/restart"
	"github.com/mirkobrombin/goup/internal/safeguard"
	"github.com/mirkobrombin/goup/internal/server/middleware"
	"github.com/mirkobrombin/goup/internal/tui"
)

func launchWebComponents(configs []config.SiteConfig, enableTUI bool, enableBench bool, wg *sync.WaitGroup) {
	// Start API Server if enabled
	if srv := api.StartAPIServer(); srv != nil {
		registerServer(srv)
	}

	// Start Dashboard Server if enabled
	if srv := dashboard.StartDashboardServer(); srv != nil {
		registerServer(srv)
	}

	// Groupping configurations by port
	portConfigs := make(map[int][]config.SiteConfig)
	for _, conf := range configs {
		portConfigs[conf.Port] = append(portConfigs[conf.Port], conf)
	}

	// Setting up loggers and TUI views
	for port, confs := range portConfigs {
		var identifier string
		if len(confs) == 1 {
			identifier = confs[0].Domain
		} else {
			identifier = fmt.Sprintf("port_%d", port)
		}

		// Set up logger
		fields := logger.Fields{"domain": identifier}
		lg, err := logger.NewLogger(identifier, fields)
		if err != nil {
			fmt.Printf("Error setting up logger for %s: %v\n", identifier, err)
			continue
		}
		loggers[identifier] = lg

		// Set up TUI view
		if enableTUI {
			tui.SetupView(identifier)
		}
	}

	// Initialize middleware manager
	mwManager := middleware.NewMiddlewareManager()
	if enableBench {
		mwManager.Use(middleware.BenchmarkMiddleware())
	}

	// Initialize plugins
	pluginManager := plugin.GetPluginManagerInstance()
	if err := pluginManager.InitPlugins(); err != nil {
		fmt.Printf("Error initializing plugins: %v\n", err)
		return
	}

	// Initialize plugins for every site up front, serially, BEFORE any server
	// starts serving. The plugin config/state maps are shared across all ports;
	// doing this from the per-port goroutines raced writers against readers in
	// HandleRequest and could crash with "concurrent map read and map write".
	for port, confs := range portConfigs {
		identifier := confs[0].Domain
		if len(confs) > 1 {
			identifier = fmt.Sprintf("port_%d", port)
		}
		lg := loggers[identifier]
		if lg == nil {
			// Logger setup failed earlier; skip this port rather than deref a
			// nil logger inside plugin init or the request path.
			continue
		}
		for _, conf := range confs {
			if err := pluginManager.InitPluginsForSite(conf, lg); err != nil {
				lg.Errorf("Error initializing plugins for site %s: %v", conf.Domain, err)
			}
		}
	}

	// Make restart drain every registered server, and let SafeGuard watch
	// memory once everything is wired up.
	restart.SetShutdownFunc(ShutdownServers)
	safeguard.Start()

	// Start servers
	for port, confs := range portConfigs {
		identifier := confs[0].Domain
		if len(confs) > 1 {
			identifier = fmt.Sprintf("port_%d", port)
		}
		if loggers[identifier] == nil {
			// No logger => plugin init was skipped; do not serve this port.
			continue
		}
		wg.Add(1)
		go func(port int, confs []config.SiteConfig) {
			defer wg.Done()
			if len(confs) == 1 {
				conf := confs[0]
				startSingleServer(conf, mwManager, pluginManager)
			} else {
				startVirtualHostServer(port, confs, mwManager, pluginManager)
			}
		}(port, confs)
	}
}
