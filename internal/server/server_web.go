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
	"github.com/mirkobrombin/goup/internal/server/middleware"
	"github.com/mirkobrombin/goup/internal/tui"
)

func launchWebComponents(configs []config.SiteConfig, enableTUI bool, enableBench bool, wg *sync.WaitGroup) {
	// Start API Server if enabled
	api.StartAPIServer()

	// Start Dashboard Server if enabled
	dashboard.StartDashboardServer()

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

	// Start servers
	for port, confs := range portConfigs {
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
