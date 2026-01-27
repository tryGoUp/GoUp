package plugins

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/internal/tools"
)

// DockerStandardConfig holds configuration for standard Docker deployments.
type DockerStandardConfig struct {
	Enable         bool              `json:"enable"`
	DockerfilePath string            `json:"dockerfile_path"`
	ImageName      string            `json:"image_name"`
	ContainerPort  string            `json:"container_port"`
	CLICommand     string            `json:"cli_command"`
	BuildArgs      map[string]string `json:"build_args"`
	RunArgs        []string          `json:"run_args"`
	ProxyPaths     []string          `json:"proxy_paths"`
}

type dockerStandardState struct {
	containerID string
	hostPort    string // dynamically assigned host port
	config      DockerStandardConfig
}

// DockerStandardPlugin manages a container based on a Dockerfile or pulled
// image and proxies requests to it.
type DockerStandardPlugin struct {
	plugin.BasePlugin
	mu     sync.Mutex
	states map[string]*dockerStandardState
}

func (d *DockerStandardPlugin) Name() string {
	return "DockerStandardPlugin"
}

func (d *DockerStandardPlugin) OnInit() error {
	d.states = make(map[string]*dockerStandardState)
	return nil
}

func (d *DockerStandardPlugin) OnInitForSite(conf config.SiteConfig, domainLogger *logger.Logger) error {
	if err := d.SetupLoggers(conf, d.Name(), domainLogger); err != nil {
		return err
	}
	var cfg DockerStandardConfig
	raw, ok := conf.PluginConfigs[d.Name()]
	if ok {
		if rawMap, ok := raw.(map[string]any); ok {
			cfg.Enable = d.IsEnabled(rawMap)
			if v, ok := rawMap["dockerfile_path"].(string); ok {
				cfg.DockerfilePath = v
			}
			if v, ok := rawMap["image_name"].(string); ok {
				cfg.ImageName = v
			}
			if v, ok := rawMap["container_port"].(string); ok {
				cfg.ContainerPort = v
			}
			if v, ok := rawMap["cli_command"].(string); ok {
				cfg.CLICommand = v
			}
			if v, ok := rawMap["build_args"].(map[string]any); ok {
				cfg.BuildArgs = make(map[string]string)
				for key, val := range v {
					if s, ok := val.(string); ok {
						cfg.BuildArgs[key] = s
					}
				}
			}
			if v, ok := rawMap["run_args"].([]any); ok {
				for _, arg := range v {
					if s, ok := arg.(string); ok {
						cfg.RunArgs = append(cfg.RunArgs, s)
					}
				}
			}
			if v, ok := rawMap["proxy_paths"].([]any); ok {
				for _, p := range v {
					if s, ok := p.(string); ok {
						cfg.ProxyPaths = append(cfg.ProxyPaths, s)
					}
				}
			}
		}
	}
	d.states[conf.Domain] = &dockerStandardState{config: cfg}
	d.DomainLogger.Infof("[DockerStandardPlugin] Initialized for domain=%s with config=%+v", conf.Domain, cfg)

	if err := d.ensureContainer(conf.Domain); err != nil {
		d.DomainLogger.Warnf("Container not started for domain %s: %v", conf.Domain, err)
	}
	return nil
}

func (d *DockerStandardPlugin) BeforeRequest(r *http.Request) {}

func (d *DockerStandardPlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	state, ok := d.states[host]
	if !ok || !state.config.Enable {
		return false
	}
	if state.containerID == "" {
		if err := d.ensureContainer(host); err != nil {
			d.PluginLogger.Errorf("Failed to start container: %v", err)
			http.Error(w, fmt.Sprintf("Failed to start container: %v", err), http.StatusInternalServerError)
			return false
		}
	}
	// Build target URL using the assigned host port.
	targetURL := fmt.Sprintf("http://0.0.0.0:%s", state.hostPort)

	if len(state.config.ProxyPaths) == 1 && state.config.ProxyPaths[0] == "/" {
		return d.proxyToContainer(targetURL, w, r)
	}
	for _, prefix := range state.config.ProxyPaths {
		if strings.HasPrefix(r.URL.Path, prefix) {
			return d.proxyToContainer(targetURL, w, r)
		}
	}
	return false
}

func (d *DockerStandardPlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}

func (d *DockerStandardPlugin) OnExit() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for domain, state := range d.states {
		if state.containerID != "" {
			out, err := RunDockerCLI(state.config.CLICommand, state.config.DockerfilePath, "rm", "-f", state.containerID)
			d.PluginLogger.Infof("Stopped container for domain %s: %s (err=%v)", domain, out, err)
			state.containerID = ""
			state.hostPort = ""
		}
	}
	return nil
}

// ensureContainer first checks if a container already exists.
// If it does, it reads the assigned host port from the container's labels (goup_port)
// and stores it in state.hostPort. Otherwise, it builds/pulls the image,
// allocates a free port, and starts the container (adding a label to record the port).
func (d *DockerStandardPlugin) ensureContainer(domain string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	state, ok := d.states[domain]
	if !ok {
		return fmt.Errorf("no configuration for domain %s", domain)
	}
	if state.containerID != "" {
		return nil
	}

	cliCmd := state.config.CLICommand
	if cliCmd == "" {
		cliCmd = "docker"
		if _, err := exec.LookPath("docker"); err != nil {
			cliCmd = "podman"
		}
	}

	// Generate unique container name using domain and container port.
	containerName := fmt.Sprintf("goup_%s_%s", domain, state.config.ContainerPort)
	d.DomainLogger.Infof("[DockerStandardPlugin] Checking for existing container with name=%s", containerName)

	// Check if a container with the expected name already exists.
	existingContainerID, err := RunDockerCLI(cliCmd, state.config.DockerfilePath, "ps", "-a",
		"--filter", fmt.Sprintf("name=%s", containerName), "--format", "{{.ID}}")
	if err == nil && strings.TrimSpace(existingContainerID) != "" {
		// Container exists; read its assigned port from the container labels.
		assignedPort, err := RunDockerCLI(cliCmd, state.config.DockerfilePath, "inspect",
			"--format", "{{ index .Config.Labels \"goup_port\"}}", containerName)
		if err != nil || strings.TrimSpace(assignedPort) == "" {
			return fmt.Errorf("container exists but goup_port label is missing or empty")
		}
		state.containerID = strings.TrimSpace(existingContainerID)
		state.hostPort = strings.TrimSpace(assignedPort)
		d.DomainLogger.Infof("[DockerStandardPlugin] Found existing container for domain=%s with ID=%s and hostPort=%s",
			domain, state.containerID, state.hostPort)
		return nil
	}

	// Build image if Dockerfile is provided; otherwise, pull the image.
	if state.config.DockerfilePath != "" {
		workDir := filepath.Dir(state.config.DockerfilePath)
		buildArgs := []string{"build", "-f", state.config.DockerfilePath, "-t", state.config.ImageName, workDir}
		for key, val := range state.config.BuildArgs {
			buildArgs = append(buildArgs, "--build-arg", fmt.Sprintf("%s=%s", key, val))
		}
		d.PluginLogger.Infof("[DockerStandardPlugin] Building image with command: %s %s", cliCmd, strings.Join(buildArgs, " "))
		buildOutput, err := RunDockerCLI(cliCmd, state.config.DockerfilePath, buildArgs...)
		if err != nil {
			return fmt.Errorf("build error: %v, output: %s", err, buildOutput)
		}
		d.PluginLogger.Infof("Build output: %s", buildOutput)
	} else {
		d.PluginLogger.Infof("[DockerStandardPlugin] Pulling image: %s", state.config.ImageName)
		pullOutput, err := RunDockerCLI(cliCmd, state.config.DockerfilePath, "pull", state.config.ImageName)
		if err != nil {
			return fmt.Errorf("pull error: %v, output: %s", err, pullOutput)
		}
		d.PluginLogger.Infof("Pull output: %s", pullOutput)
	}

	// Allocate a free host port from a high range.
	hostPort, err := tools.GetFreePort()
	if err != nil {
		return fmt.Errorf("failed to get free port: %v", err)
	}

	// Run container with host port mapping using the free port,
	// and add a label to record the assigned port.
	runArgs := []string{
		"run", "-d",
		"--name", containerName,
		"-p", fmt.Sprintf("%s:%s", hostPort, state.config.ContainerPort),
		"--label", fmt.Sprintf("goup_port=%s", hostPort),
	}
	runArgs = append(runArgs, state.config.RunArgs...)
	runArgs = append(runArgs, state.config.ImageName)

	d.PluginLogger.Infof("[DockerStandardPlugin] Running container with command: %s %s", cliCmd, strings.Join(runArgs, " "))
	runOutput, err := RunDockerCLI(cliCmd, state.config.DockerfilePath, runArgs...)
	if err != nil {
		return fmt.Errorf("run error: %v, output: %s", err, runOutput)
	}
	state.containerID = strings.TrimSpace(runOutput)
	state.hostPort = hostPort
	return nil
}

func (d *DockerStandardPlugin) proxyToContainer(targetURL string, w http.ResponseWriter, r *http.Request) bool {
	d.DomainLogger.Infof("[DockerStandardPlugin] Proxying request to: %s", targetURL)
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		d.PluginLogger.Errorf("Error parsing URL: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return true
	}
	proxy := httputil.NewSingleHostReverseProxy(parsedURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, e error) {
		d.PluginLogger.Errorf("Proxy error: %v", e)
		http.Error(w, "Proxy error", http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
	return true
}
