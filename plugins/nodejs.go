package plugins

import (
	"fmt"
	"io"
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
	process     *os.Process
	siteConfigs map[string]NodeJSPluginConfig
}

func (p *NodeJSPlugin) Name() string {
	return "NodeJSPlugin"
}

func (p *NodeJSPlugin) OnInit() error {
	p.siteConfigs = make(map[string]NodeJSPluginConfig)
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
	p.ensureNodeServerRunning(cfg)

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
	if p.process != nil {
		// Log to the plugin logger only
		p.PluginLogger.Infof("[NodeJSPlugin] Terminating Node.js process (PID=%d).", p.process.Pid)
		_ = p.process.Kill()
		p.process = nil
	}
	return nil
}

// ensureNodeServerRunning starts Node.js if it is not already running.
func (p *NodeJSPlugin) ensureNodeServerRunning(cfg NodeJSPluginConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.process != nil {
		return
	}

	p.PluginLogger.Infof("Starting Node.js server...")

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
		p.PluginLogger.Errorf("Failed to start Node.js server: %v", err)
		return
	}

	p.process = cmd.Process
	p.PluginLogger.Infof("Started Node.js server (PID=%d) on port %s", p.process.Pid, cfg.Port)

	// Watch for process exit.
	go func() {
		err := cmd.Wait()
		p.PluginLogger.Infof("Node.js server exited (PID=%d), err=%v", p.process.Pid, err)
		p.PluginLogger.Writer().Close()

		p.mu.Lock()
		p.process = nil
		p.mu.Unlock()
	}()
}

// proxyToNode forwards the request to Node.js and sends back the response.
func (p *NodeJSPlugin) proxyToNode(w http.ResponseWriter, r *http.Request, cfg NodeJSPluginConfig) {
	nodeURL := fmt.Sprintf("http://localhost:%s%s", cfg.Port, r.URL.Path)
	if r.URL.RawQuery != "" {
		nodeURL += "?" + r.URL.RawQuery
	}

	bodyReader, err := io.ReadAll(r.Body)
	if err != nil {
		p.PluginLogger.Errorf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	req, err := http.NewRequest(r.Method, nodeURL, strings.NewReader(string(bodyReader)))
	if err != nil {
		p.PluginLogger.Errorf("Failed to create request for Node.js: %v", err)
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		p.PluginLogger.Errorf("Failed to connect to Node.js backend: %v", err)
		http.Error(w, "Node.js backend unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward response headers.
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.PluginLogger.Errorf("Failed to read response from Node.js: %v", err)
		http.Error(w, "Failed to read response from Node.js", http.StatusInternalServerError)
		return
	}
	w.Write(body)
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
