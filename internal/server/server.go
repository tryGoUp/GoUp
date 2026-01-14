package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/armon/go-radix"
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

var (
	loggers = make(map[string]*logger.Logger)
	tuiMode bool
)

// StartServers starts the servers based on the provided configurations.
func StartServers(configs []config.SiteConfig, enableTUI bool, enableBench bool) {
	tuiMode = enableTUI

	// FIXME: move all TUI related code out of this package, I do not feel
	// comfortable having it here, leads to confusion.
	if tuiMode {
		tui.InitTUI()
	}

	// Initialize the global async logger
	middleware.InitAsyncLogger(10000)

	// Start API Server if enabled
	api.StartAPIServer()

	// Start SafeGuard
	safeguard.Start()

	// Start Dashboard Server if enabled
	dashboard.StartDashboardServer()

	// Groupping configurations by port to minimize the number of servers
	// NOTE: configurations with the same port are treated as virtual hosts
	// so they will be served by the same server instance.
	portConfigs := make(map[int][]config.SiteConfig)
	for _, conf := range configs {
		portConfigs[conf.Port] = append(portConfigs[conf.Port], conf)
	}

	// Setting up loggers and TUI views before starting servers so that
	// they are ready to host the messages.
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
		if tuiMode {
			tui.SetupView(identifier)
		}
	}

	// Initialize the global middleware manager
	mwManager := middleware.NewMiddlewareManager()

	// Enable benchmark middleware if requested
	if enableBench {
		mwManager.Use(middleware.BenchmarkMiddleware())
	}

	// Initialize the plugins globally
	pluginManager := plugin.GetPluginManagerInstance()
	if err := pluginManager.InitPlugins(); err != nil {
		fmt.Printf("Error initializing plugins: %v\n", err)
		return
	}

	var wg sync.WaitGroup

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

	// Start TUI if enabled
	if tuiMode {
		tui.Run()
	} else {
		// Let's keep alive the main goroutine alive
		wg.Wait()
	}
}

func anyHasSSL(confs []config.SiteConfig) bool {
	for _, c := range confs {
		if c.SSL.Enabled {
			return true
		}
	}
	return false
}

// startSingleServer starts a server for a single site configuration.
func startSingleServer(conf config.SiteConfig, mwManager *middleware.MiddlewareManager, pm *plugin.PluginManager) {
	identifier := conf.Domain
	lg := loggers[identifier]

	// We do not want to start a server if the root directory does not exist
	// let's fail fast instead.
	if conf.ProxyPass == "" {
		// Here we allow empty paths as RootDirectory, this is useful for
		// proxying requests to other servers by default, like Flask apps.
		if conf.RootDirectory != "" {
			if _, err := os.Stat(conf.RootDirectory); os.IsNotExist(err) {
				lg.Errorf("Root directory does not exist for %s: %v", conf.Domain, err)
				return
			}
		}
	}

	// Initialize plugins for this site
	if err := pm.InitPluginsForSite(conf, lg); err != nil {
		lg.Errorf("Error initializing plugins for site %s: %v", conf.Domain, err)
		return
	}

	// Add plugin middleware
	mwManagerCopy := mwManager.Copy()
	mwManagerCopy.Use(plugin.PluginMiddleware(pm))

	handler, err := createHandler(conf, lg, identifier, mwManagerCopy)
	if err != nil {
		lg.Errorf("Error creating handler for %s: %v", conf.Domain, err)
		return
	}

	server := createHTTPServer(conf, handler)
	restart.SetServer(server)
	startServerInstance(server, conf, lg)
}

// startVirtualHostServer starts a server that handles multiple domains on the same port.
func startVirtualHostServer(port int, configs []config.SiteConfig, mwManager *middleware.MiddlewareManager, pm *plugin.PluginManager) {
	identifier := fmt.Sprintf("port_%d", port)
	lg := loggers[identifier]

	radixTree := radix.New()

	for _, conf := range configs {
		if conf.ProxyPass == "" && conf.RootDirectory != "" {
			if _, err := os.Stat(conf.RootDirectory); os.IsNotExist(err) {
				lg.Errorf("Root directory does not exist for %s: %v", conf.Domain, err)
			}
		}

		if err := pm.InitPluginsForSite(conf, lg); err != nil {
			lg.Errorf("Error initializing plugins for site %s: %v", conf.Domain, err)
			continue
		}

		mwManagerCopy := mwManager.Copy()
		mwManagerCopy.Use(plugin.PluginMiddleware(pm))

		handler, err := createHandler(conf, lg, identifier, mwManagerCopy)
		if err != nil {
			lg.Errorf("Error creating handler for %s: %v", conf.Domain, err)
			continue
		}

		radixTree.Insert(conf.Domain, handler)
	}

	serverConf := config.SiteConfig{Port: port}

	mainHandler := func(w_ http.ResponseWriter, r_ *http.Request) {
		host := r_.Host
		if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
			host = host[:colonIndex]
		}
		if h, found := radixTree.Get(host); found {
			h.(http.Handler).ServeHTTP(w_, r_)
		} else {
			http.NotFound(w_, r_)
		}
	}

	server := createHTTPServer(serverConf, http.HandlerFunc(mainHandler))
	startServerInstance(server, serverConf, lg)
}
