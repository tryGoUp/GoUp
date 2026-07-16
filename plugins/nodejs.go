package plugins

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
)

// NodeJSPluginConfig represents the configuration for the NodeJSPlugin.
type NodeJSPluginConfig struct {
	Enable         bool     `json:"enable"`
	Port           string   `json:"port"`
	RootDir        string   `json:"root_dir"`
	Entry          string   `json:"entry"`
	InstallDeps    bool     `json:"install_deps"`
	NodePath       string   `json:"node_path"`
	PackageManager string   `json:"package_manager"`
	ProxyPaths     []string `json:"proxy_paths"`
}

// NodeJSPlugin handles the execution of a Node.js application.
type NodeJSPlugin struct {
	plugin.BasePlugin

	mu          sync.Mutex
	processes   map[string]*os.Process // per-domain Node.js process
	siteConfigs map[string]NodeJSPluginConfig
}

func (p *NodeJSPlugin) Name() string {
	return "NodeJSPlugin"
}

func (p *NodeJSPlugin) OnInit() error {
	p.siteConfigs = make(map[string]NodeJSPluginConfig)
	p.processes = make(map[string]*os.Process)
	return nil
}

func (p *NodeJSPlugin) OnInitForSite(conf config.SiteConfig, domainLogger *logger.Logger) error {
	if err := p.SetupLoggers(conf, p.Name(), domainLogger); err != nil {
		return err
	}

	pluginConfigRaw, ok := conf.PluginConfigs[p.Name()]
	if !ok {
		p.siteConfigs[conf.Domain] = NodeJSPluginConfig{}
		return nil
	}
	cfg := NodeJSPluginConfig{}
	if rawMap, ok := pluginConfigRaw.(map[string]any); ok {
		// Use BasePlugin's IsEnabled method to determine if the plugin is enabled.
		cfg.Enable = p.IsEnabled(rawMap)
		if port, ok := rawMap["port"].(string); ok {
			cfg.Port = port
		}
		if rootDir, ok := rawMap["root_dir"].(string); ok {
			cfg.RootDir = rootDir
		}
		if entry, ok := rawMap["entry"].(string); ok {
			cfg.Entry = entry
		}
		if installDeps, ok := rawMap["install_deps"].(bool); ok {
			cfg.InstallDeps = installDeps
		}
		if nodePath, ok := rawMap["node_path"].(string); ok {
			cfg.NodePath = nodePath
		}
		if packageManager, ok := rawMap["package_manager"].(string); ok {
			cfg.PackageManager = packageManager
		}
		if proxyPaths, ok := rawMap["proxy_paths"].([]any); ok {
			for _, pathVal := range proxyPaths {
				if pathStr, ok := pathVal.(string); ok {
					cfg.ProxyPaths = append(cfg.ProxyPaths, pathStr)
				}
			}
		}
	}
	p.siteConfigs[conf.Domain] = cfg
	return nil
}

func (p *NodeJSPlugin) BeforeRequest(r *http.Request) {}

func (p *NodeJSPlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	// Identify the domain and strip any port.
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	cfg, ok := p.siteConfigs[host]
	if !ok || !cfg.Enable {
		return false
	}

	// Ensure Node.js is running if needed.
	p.ensureNodeServerRunning(host, cfg)

	// Check if path matches one of the ProxyPaths.
	for _, proxyPath := range cfg.ProxyPaths {
		if strings.HasPrefix(r.URL.Path, proxyPath) {
			p.DomainLogger.Infof("[NodeJSPlugin] Delegating path=%s to Node.js (domain=%s)", r.URL.Path, host)
			p.proxyToNode(w, r, cfg)
			return true
		}
	}
	return false
}

func (p *NodeJSPlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}

func (p *NodeJSPlugin) OnExit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for domain, proc := range p.processes {
		if proc != nil {
			// Log to the plugin logger only
			p.PluginLogger.Infof("[NodeJSPlugin] Terminating Node.js process for domain=%s (PID=%d).", domain, proc.Pid)
			_ = proc.Kill()
			p.processes[domain] = nil
		}
	}
	return nil
}

// ensureNodeServerRunning starts a Node.js process for the given domain if one
// is not already running.
func (p *NodeJSPlugin) ensureNodeServerRunning(domain string, cfg NodeJSPluginConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.processes[domain] != nil {
		return
	}

	p.PluginLogger.Infof("Starting Node.js server for domain=%s...", domain)

	// Install dependencies if required.
	if cfg.InstallDeps {
		p.installDependencies(cfg)
	}

	entryPath := filepath.Join(cfg.RootDir, cfg.Entry)
	nodePath := cfg.NodePath
	if nodePath == "" {
		nodePath = "node"
	}

	cmd := exec.Command(nodePath, entryPath)
	cmd.Dir = cfg.RootDir
	cmd.Stdout = p.PluginLogger.Writer()
	cmd.Stderr = p.PluginLogger.Writer()

	if err := cmd.Start(); err != nil {
		p.PluginLogger.Errorf("Failed to start Node.js server for domain=%s: %v", domain, err)
		return
	}

	p.processes[domain] = cmd.Process
	p.PluginLogger.Infof("Started Node.js server for domain=%s (PID=%d) on port %s", domain, cmd.Process.Pid, cfg.Port)

	// Watch for process exit. Capture cmd so we read its Pid without racing the
	// map reset below.
	go func(dom string, c *exec.Cmd) {
		err := c.Wait()
		p.PluginLogger.Infof("Node.js server exited for domain=%s (PID=%d), err=%v", dom, c.Process.Pid, err)

		p.mu.Lock()
		p.processes[dom] = nil
		p.mu.Unlock()
	}(domain, cmd)
}

// proxyToNode forwards the request to Node.js, streaming both bodies through
// a shared reverse proxy instead of buffering them in memory.
func (p *NodeJSPlugin) proxyToNode(w http.ResponseWriter, r *http.Request, cfg NodeJSPluginConfig) {
	proxy, err := upstreamProxy("http://localhost:"+cfg.Port, p.PluginLogger)
	if err != nil {
		p.PluginLogger.Errorf("Failed to create proxy for Node.js: %v", err)
		http.Error(w, "Node.js backend unavailable", http.StatusBadGateway)
		return
	}
	proxy.ServeHTTP(w, r)
}

// installDependencies installs dependencies using the configured package manager.
func (p *NodeJSPlugin) installDependencies(cfg NodeJSPluginConfig) {
	nodeModulesPath := filepath.Join(cfg.RootDir, "node_modules")
	if _, err := os.Stat(nodeModulesPath); os.IsNotExist(err) {
		p.PluginLogger.Infof("node_modules not found, installing dependencies in %s", cfg.RootDir)
		pm := cfg.PackageManager
		if pm == "" {
			pm = "npm"
		}
		cmd := exec.Command(pm, "install")
		cmd.Dir = cfg.RootDir
		cmd.Stdout = p.PluginLogger.Writer()
		cmd.Stderr = p.PluginLogger.Writer()

		if err := cmd.Run(); err != nil {
			p.PluginLogger.Errorf("Failed to install dependencies using %s: %v", pm, err)
		} else {
			p.PluginLogger.Infof("Dependencies installed successfully using %s", pm)
		}
	}
}
