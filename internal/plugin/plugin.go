package plugin

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/server/middleware"
)

// Plugin defines the interface for GoUP plugins.
type Plugin interface {
	// Name returns the plugin's name.
	Name() string
	// OnInit is called once during the global plugin initialization.
	OnInit() error
	// OnInitForSite is called for each site configuration.
	OnInitForSite(conf config.SiteConfig, logger *logger.Logger) error
	// BeforeRequest is invoked before serving each request.
	BeforeRequest(r *http.Request)
	// HandleRequest can fully handle the request, returning true if it does so.
	HandleRequest(w http.ResponseWriter, r *http.Request) bool
	// AfterRequest is invoked after the request has been served or handled.
	AfterRequest(w http.ResponseWriter, r *http.Request)
	// OnExit is called when the server is shutting down.
	OnExit() error
}

// PluginManager manages loading and initialization of plugins.
type PluginManager struct {
	mu      sync.Mutex
	plugins []Plugin
	active  map[string]bool

	// snapshot holds an immutable copy of the registered plugins so the
	// request hot path can dispatch without taking the lock or copying.
	snapshot atomic.Pointer[[]Plugin]
}

var (
	// DefaultPluginManager is the default instance used by the application.
	DefaultPluginManager *PluginManager

	progressBarRunning  bool
	progressBarStopChan chan bool
	progressBarLock     sync.Mutex
)

// NewPluginManager creates a new PluginManager instance.
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins: []Plugin{},
		active:  make(map[string]bool),
	}
}

// SetDefaultPluginManager sets the default PluginManager instance.
func SetDefaultPluginManager(pm *PluginManager) {
	DefaultPluginManager = pm
}

// GetPluginManagerInstance returns the default PluginManager instance.
func GetPluginManagerInstance() *PluginManager {
	if DefaultPluginManager == nil {
		DefaultPluginManager = NewPluginManager()
	}
	return DefaultPluginManager
}

// Register registers a new plugin.
func (pm *PluginManager) Register(p Plugin) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.plugins = append(pm.plugins, p)
	pm.active[p.Name()] = isPluginEnabled(p.Name())

	snap := make([]Plugin, len(pm.plugins))
	copy(snap, pm.plugins)
	pm.snapshot.Store(&snap)
}

// InitPlugins calls OnInit on all registered plugins.
func (pm *PluginManager) InitPlugins() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, plugin := range pm.plugins {
		if !isPluginEnabled(plugin.Name()) {
			fmt.Printf("Plugin %s is disabled\n", plugin.Name())
			continue
		}
		if err := plugin.OnInit(); err != nil {
			return err
		}
	}
	return nil
}

// InitPluginsForSite calls OnInitForSite on all plugins.
func (pm *PluginManager) InitPluginsForSite(conf config.SiteConfig, l *logger.Logger) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, plugin := range pm.plugins {
		if !isPluginEnabled(plugin.Name()) {
			continue
		}
		if err := plugin.OnInitForSite(conf, l); err != nil {
			return err
		}
	}
	return nil
}

// GetRegisteredPlugins returns the names of all registered plugins.
func (pm *PluginManager) GetRegisteredPlugins() []string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	names := make([]string, len(pm.plugins))
	for i, plugin := range pm.plugins {
		names[i] = plugin.Name()
	}
	return names
}

func isPluginEnabled(name string) bool {
	config.GlobalConfMu.RLock()
	defer config.GlobalConfMu.RUnlock()
	if config.GlobalConf == nil || len(config.GlobalConf.EnabledPlugins) == 0 {
		return true
	}
	for _, pName := range config.GlobalConf.EnabledPlugins {
		if pName == name {
			return true
		}
	}
	return false
}

// PluginMiddleware applies the plugin hooks around each HTTP request.
func PluginMiddleware(pm *PluginManager) middleware.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read the immutable snapshot: no lock, no copy per request.
			var registered []Plugin
			if snap := pm.snapshot.Load(); snap != nil {
				registered = *snap
			}

			// BeforeRequest
			for _, plugin := range registered {
				plugin.BeforeRequest(r)
			}

			// HandleRequest (plugins may intercept the request)
			var handled bool
			for _, plugin := range registered {
				if plugin.HandleRequest(w, r) {
					handled = true
					break
				}
			}

			// Proceed to next if not fully handled
			if !handled {
				next.ServeHTTP(w, r)
			}

			// AfterRequest
			for _, plugin := range registered {
				plugin.AfterRequest(w, r)
			}
		})
	}
}

// ShowProgressBar displays a basic spinner on stdout for a long-running task.
func ShowProgressBar(taskName string) {
	progressBarLock.Lock()
	defer progressBarLock.Unlock()
	if progressBarRunning {
		return
	}
	progressBarRunning = true
	progressBarStopChan = make(chan bool)

	go func() {
		chars := []rune{'|', '/', '-', '\\'}
		idx := 0
		for {
			select {
			case <-progressBarStopChan:
				return
			default:
				fmt.Printf("\r%s %c", taskName, chars[idx])
				idx = (idx + 1) % len(chars)
				time.Sleep(150 * time.Millisecond)
			}
		}
	}()
}

// HideProgressBar stops the spinner and clears the line.
func HideProgressBar() {
	progressBarLock.Lock()
	defer progressBarLock.Unlock()
	if !progressBarRunning {
		return
	}
	progressBarStopChan <- true
	close(progressBarStopChan)
	progressBarRunning = false
	fmt.Printf("\r\033[K")
}
