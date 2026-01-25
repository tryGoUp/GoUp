package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/armon/go-radix"
	"github.com/mirkobrombin/goup/internal/assets"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/internal/restart"
	"github.com/mirkobrombin/goup/internal/server/middleware"
	"github.com/mirkobrombin/goup/internal/tui"
)

// ServerMode defines which components to start.
type ServerMode int

const (
	ModeWeb ServerMode = 1 << iota
	ModeDNS
	ModeAll = ModeWeb | ModeDNS
)

var (
	loggers = make(map[string]*logger.Logger)
	tuiMode bool
)

// StartServers starts the servers based on the provided configurations and mode.
func StartServers(configs []config.SiteConfig, enableTUI bool, enableBench bool, mode ServerMode) {
	tuiMode = enableTUI

	// FIXME: move all TUI related code out of this package, I do not feel
	// comfortable having it here, leads to confusion.
	if tuiMode {
		tui.InitTUI()
	}

	// Initialize the global async logger
	middleware.InitAsyncLogger(10000)

	var wg sync.WaitGroup

	// Start DNS Server if requested (and available)
	if mode&ModeDNS != 0 {
		launchDNS(&wg)
	}

	// Start Web Server if requested (and available)
	if mode&ModeWeb != 0 {
		launchWebComponents(configs, enableTUI, enableBench, &wg)
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

	var firstHandler http.Handler

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

		if firstHandler == nil {
			firstHandler = handler
		}

		radixTree.Insert(conf.Domain, handler)
	}

	serverConf := config.SiteConfig{Port: port}

	mainHandler := func(w_ http.ResponseWriter, r_ *http.Request) {
		host, _, err := net.SplitHostPort(r_.Host)
		if err != nil {
			// Host might not have a port (e.g. "example.com")
			host = r_.Host
		}

		if h, found := radixTree.Get(host); found {
			h.(http.Handler).ServeHTTP(w_, r_)
		} else {
			if firstHandler != nil {
				firstHandler.ServeHTTP(w_, r_)
				return
			}
			assets.RenderErrorPage(w_, http.StatusNotFound, "Page Not Found", "The page you are looking for does not exist.")
		}
	}

	server := createHTTPServer(serverConf, http.HandlerFunc(mainHandler))
	startServerInstance(server, serverConf, lg)
}
