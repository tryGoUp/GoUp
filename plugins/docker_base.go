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
	"strings"
	"sync"
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

// DockerBasePlugin provides common Docker functionality.
type DockerBasePlugin struct {
	plugin.BasePlugin
	mu     sync.Mutex
	Config DockerBaseConfig
}

func (d *DockerBasePlugin) Name() string {
	return "DockerBasePlugin"
}

func (d *DockerBasePlugin) OnInit() error {
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
	d.Config = cfg

	// Determine CLICommand if not set.
	if d.Config.CLICommand == "" {
		if _, err := exec.LookPath("docker"); err == nil {
			d.Config.CLICommand = "docker"
		} else if _, err := exec.LookPath("podman"); err == nil {
			d.Config.CLICommand = "podman"
		} else {
			return fmt.Errorf("neither 'docker' nor 'podman' found in PATH")
		}
	}

	// Set default SocketPath.
	if strings.ToLower(d.Config.CLICommand) == "podman" && d.Config.SocketPath == "" {
		userSocket := fmt.Sprintf("/run/user/%d/podman/podman.sock", os.Getuid())
		if _, err := os.Stat(userSocket); err == nil {
			d.Config.SocketPath = userSocket
		} else {
			d.Config.SocketPath = "/run/podman/podman.sock"
		}
	}
	if d.Config.SocketPath == "" {
		d.Config.SocketPath = "/var/run/docker.sock"
	}

	d.DomainLogger.Infof("[DockerBasePlugin] Initialized for domain=%s, mode=%s, CLICommand=%s, SocketPath=%s",
		conf.Domain, d.Config.Mode, d.Config.CLICommand, d.Config.SocketPath)
	return nil
}

func (d *DockerBasePlugin) BeforeRequest(r *http.Request) {}

func (d *DockerBasePlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/docker/") {
		return false
	}
	output, err := d.ListContainers()
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

// ListContainers lists containers via Docker API; falls back to CLI if needed.
func (d *DockerBasePlugin) ListContainers() (string, error) {
	res, err := d.callDockerAPI("GET", "/containers/json", nil)
	if err == nil {
		return res, nil
	}
	// CLI fallback.
	if strings.ToLower(d.Config.CLICommand) == "podman" {
		return RunDockerCLI(d.Config.CLICommand, d.Config.DockerfilePath, "ps", "--format", "json")
	}
	return RunDockerCLI(d.Config.CLICommand, d.Config.DockerfilePath, "ps", "--format", "{{json .}}")
}

func (d *DockerBasePlugin) callDockerAPI(method, path string, body []byte) (string, error) {
	d.DomainLogger.Infof("[DockerBasePlugin] Calling Docker API: %s %s", method, path)
	socket := d.Config.SocketPath
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("unix", socket)
		},
	}
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
