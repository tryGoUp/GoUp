package plugins

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
)

// DockerBaseConfig holds configuration for Docker/Podman integration.
type DockerBaseConfig struct {
	Enable         bool   `json:"enable"`
	Mode           string `json:"mode"`
	ComposeFile    string `json:"compose_file"`
	DockerfilePath string `json:"dockerfile_path"`
	SocketPath     string `json:"socket_path"`
	CLICommand     string `json:"cli_command"`
}

// DockerBasePlugin provides common Docker functionality. siteConfigs is
// populated serially during startup and only read while serving, so no lock is
// required.
type DockerBasePlugin struct {
	plugin.BasePlugin
	siteConfigs map[string]DockerBaseConfig
}

func (d *DockerBasePlugin) Name() string {
	return "DockerBasePlugin"
}

func (d *DockerBasePlugin) OnInit() error {
	d.siteConfigs = make(map[string]DockerBaseConfig)
	return nil
}

func (d *DockerBasePlugin) OnInitForSite(conf config.SiteConfig, domainLogger *logger.Logger) error {
	if err := d.SetupLoggers(conf, d.Name(), domainLogger); err != nil {
		return err
	}

	var cfg DockerBaseConfig
	raw, ok := conf.PluginConfigs[d.Name()]
	if ok {
		if rawMap, ok := raw.(map[string]any); ok {
			cfg.Enable = d.IsEnabled(rawMap)
			if v, ok := rawMap["mode"].(string); ok {
				cfg.Mode = v
			}
			if v, ok := rawMap["compose_file"].(string); ok {
				cfg.ComposeFile = v
			}
			if v, ok := rawMap["dockerfile_path"].(string); ok {
				cfg.DockerfilePath = v
			}
			if v, ok := rawMap["socket_path"].(string); ok {
				cfg.SocketPath = v
			}
			if v, ok := rawMap["cli_command"].(string); ok {
				cfg.CLICommand = v
			}
		}
	}

	// If the Docker plugin is not enabled (or not configured) for this site,
	// store the disabled config and skip CLI resolution entirely so that a
	// missing docker/podman binary does not break plugin init on machines that
	// don't need Docker.
	if !cfg.Enable {
		d.siteConfigs[conf.Domain] = cfg
		return nil
	}

	// Determine CLICommand if not set.
	if cfg.CLICommand == "" {
		if _, err := exec.LookPath("docker"); err == nil {
			cfg.CLICommand = "docker"
		} else if _, err := exec.LookPath("podman"); err == nil {
			cfg.CLICommand = "podman"
		} else {
			return fmt.Errorf("neither 'docker' nor 'podman' found in PATH")
		}
	}

	// Set default SocketPath.
	if runtime.GOOS != "windows" && strings.ToLower(cfg.CLICommand) == "podman" && cfg.SocketPath == "" {
		userSocket := fmt.Sprintf("/run/user/%d/podman/podman.sock", os.Getuid())
		if _, err := os.Stat(userSocket); err == nil {
			cfg.SocketPath = userSocket
		} else {
			cfg.SocketPath = "/run/podman/podman.sock"
		}
	}
	if runtime.GOOS != "windows" && cfg.SocketPath == "" {
		cfg.SocketPath = "/var/run/docker.sock"
	}

	d.siteConfigs[conf.Domain] = cfg
	d.DomainLogger.Infof("[DockerBasePlugin] Initialized for domain=%s, mode=%s, CLICommand=%s, SocketPath=%s",
		conf.Domain, cfg.Mode, cfg.CLICommand, cfg.SocketPath)
	return nil
}

func (d *DockerBasePlugin) BeforeRequest(r *http.Request) {}

func (d *DockerBasePlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/docker/") {
		return false
	}

	// Gate the container-listing endpoint behind the site's own configuration:
	// it must be explicitly enabled for the requested host, otherwise the
	// Docker inventory would be exposed on every site.
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	cfg, ok := d.siteConfigs[host]
	if !ok || !cfg.Enable {
		return false
	}

	output, err := d.listContainers(cfg)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error listing containers: %v", err), http.StatusInternalServerError)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(output))
	return true
}

func (d *DockerBasePlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}

func (d *DockerBasePlugin) OnExit() error {
	return nil
}

// listContainers lists containers via Docker API; falls back to CLI if needed.
func (d *DockerBasePlugin) listContainers(cfg DockerBaseConfig) (string, error) {
	res, err := d.callDockerAPI(cfg, "GET", "/containers/json", nil)
	if err == nil {
		return res, nil
	}
	// CLI fallback.
	if strings.ToLower(cfg.CLICommand) == "podman" {
		return RunDockerCLI(cfg.CLICommand, cfg.DockerfilePath, "ps", "--format", "json")
	}
	return RunDockerCLI(cfg.CLICommand, cfg.DockerfilePath, "ps", "--format", "{{json .}}")
}

func (d *DockerBasePlugin) callDockerAPI(cfg DockerBaseConfig, method, path string, body []byte) (string, error) {
	d.DomainLogger.Infof("[DockerBasePlugin] Calling Docker API: %s %s", method, path)
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("docker API over Unix socket is not supported on Windows")
	}
	socket := cfg.SocketPath
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("unix", socket)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	urlStr := "http://unix" + path
	req, err := http.NewRequest(method, urlStr, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return string(data), err
}

// RunDockerCLI executes a Docker/Podman CLI command.
func RunDockerCLI(cliCommand, dockerfilePath string, args ...string) (string, error) {
	cmd := exec.Command(cliCommand, args...)
	workDir := filepath.Dir(dockerfilePath)
	if workDir == "" {
		workDir = "."
	}
	cmd.Dir = workDir

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout

	err := cmd.Run()
	return stdout.String(), err
}

// GetRunningContainer returns the running container ID for the given image.
func GetRunningContainer(cliCommand, dockerfilePath, imageName string) (string, error) {
	output, err := RunDockerCLI(cliCommand, dockerfilePath, "ps", "--filter", fmt.Sprintf("ancestor=%s", imageName), "--format", "{{.ID}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}
