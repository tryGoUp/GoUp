package plugins

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
)

type PythonPluginConfig struct {
	Enable         bool              `json:"enable"`
	Port           string            `json:"port"`
	RootDir        string            `json:"root_dir"`
	AppType        string            `json:"app_type"`
	Command        string            `json:"command"`
	PackageManager string            `json:"package_manager"`
	InstallDeps    bool              `json:"install_deps"`
	EnvVars        map[string]string `json:"env_vars"`
	ProxyPaths     []string          `json:"proxy_paths"`
	UseVenv        bool              `json:"use_venv"`
}

type pythonProcessState struct {
	process *os.Process
	config  PythonPluginConfig
}

type PythonPlugin struct {
	plugin.BasePlugin

	mu        sync.Mutex
	processes map[string]*pythonProcessState
}

func (p *PythonPlugin) Name() string {
	return "PythonPlugin"
}

func (p *PythonPlugin) OnInit() error {
	p.processes = make(map[string]*pythonProcessState)
	return nil
}

func (p *PythonPlugin) OnInitForSite(conf config.SiteConfig, baseLogger *logger.Logger) error {
	if err := p.SetupLoggers(conf, p.Name(), baseLogger); err != nil {
		return err
	}

	raw, ok := conf.PluginConfigs[p.Name()]
	if !ok {
		p.processes[conf.Domain] = &pythonProcessState{config: PythonPluginConfig{}}
		return nil
	}

	cfg := PythonPluginConfig{}
	if rawMap, ok := raw.(map[string]any); ok {
		// Use BasePlugin's IsEnabled method to determine if the plugin is enabled.
		cfg.Enable = p.IsEnabled(rawMap)
		if v, ok := rawMap["port"].(string); ok {
			cfg.Port = v
		}
		if v, ok := rawMap["root_dir"].(string); ok {
			cfg.RootDir = v
		}
		if v, ok := rawMap["app_type"].(string); ok {
			cfg.AppType = v
		}
		if v, ok := rawMap["command"].(string); ok {
			cfg.Command = v
		}
		if v, ok := rawMap["package_manager"].(string); ok {
			cfg.PackageManager = v
		}
		if v, ok := rawMap["install_deps"].(bool); ok {
			cfg.InstallDeps = v
		}
		if envVars, ok := rawMap["env_vars"].(map[string]any); ok {
			tmp := make(map[string]string)
			for k, val := range envVars {
				if s, ok := val.(string); ok {
					tmp[k] = s
				}
			}
			cfg.EnvVars = tmp
		}
		if proxyPaths, ok := rawMap["proxy_paths"].([]any); ok {
			for _, pathVal := range proxyPaths {
				if pathStr, ok := pathVal.(string); ok {
					cfg.ProxyPaths = append(cfg.ProxyPaths, pathStr)
				}
			}
		}
		if uv, ok := rawMap["use_venv"].(bool); ok {
			cfg.UseVenv = uv
		}
	}
	p.processes[conf.Domain] = &pythonProcessState{config: cfg}
	return nil
}

func (p *PythonPlugin) BeforeRequest(r *http.Request) {}

func (p *PythonPlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	st, ok := p.processes[host]
	if !ok || !st.config.Enable {
		return false
	}

	p.ensurePythonProcess(host)

	if len(st.config.ProxyPaths) == 1 && st.config.ProxyPaths[0] == "/" {
		p.proxyToPython(host, w, r)
		return true
	}
	for _, prefix := range st.config.ProxyPaths {
		if strings.HasPrefix(r.URL.Path, prefix) {
			p.DomainLogger.Infof("[PythonPlugin] Delegating path=%s to python, domain=%s", r.URL.Path, host)
			p.proxyToPython(host, w, r)
			return true
		}
	}
	return false
}

func (p *PythonPlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}

func (p *PythonPlugin) OnExit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for domain, st := range p.processes {
		if st.process != nil {
			p.PluginLogger.Infof("Terminating Python process for domain '%s' (PID=%d)", domain, st.process.Pid)
			_ = st.process.Kill()
			st.process = nil
		}
	}
	return nil
}

func (p *PythonPlugin) ensurePythonProcess(domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	st := p.processes[domain]
	if st == nil || st.config.Port == "" || st.process != nil {
		return
	}

	p.DomainLogger.Infof("[PythonPlugin] Starting python server for domain=%s ...", domain)
	pythonCmd := st.config.Command
	if pythonCmd == "" {
		pythonCmd = "python"
		if _, err := exec.LookPath("python3"); err == nil {
			pythonCmd = "python3"
		}
	}

	var venvPy string
	if st.config.UseVenv {
		venvPy = p.setupVenv(st.config, pythonCmd)
		if venvPy != "" {
			pythonCmd = venvPy
		} else {
			p.PluginLogger.Warnf("Failed to setup venv, fallback to system python: %s", pythonCmd)
		}
	}

	if st.config.InstallDeps {
		p.installDeps(st.config, pythonCmd)
	}

	var args []string
	switch strings.ToLower(st.config.AppType) {
	case "flask":
		args = []string{"-m", "flask", "run", "--host=0.0.0.0", "--port=" + st.config.Port}
	case "django":
		args = []string{"manage.py", "runserver", "0.0.0.0:" + st.config.Port}
	default:
		entryFile := filepath.Join(st.config.RootDir, "app.py")
		args = []string{entryFile}
	}

	cmd := exec.Command(pythonCmd, args...)
	cmd.Dir = st.config.RootDir

	envList := os.Environ()
	for k, v := range st.config.EnvVars {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = envList

	// Write python stdout to the plugin logger
	cmd.Stdout = p.PluginLogger.Writer()
	cmd.Stderr = p.PluginLogger.Writer()

	if err := cmd.Start(); err != nil {
		p.PluginLogger.Errorf("Failed to start Python process for '%s': %v", domain, err)
		return
	}

	st.process = cmd.Process
	p.PluginLogger.Infof("Started Python server for domain '%s' (PID=%d) on port %s",
		domain, st.process.Pid, st.config.Port)

	go func(dom string, c *exec.Cmd) {
		err := c.Wait()
		p.PluginLogger.Infof("Python server exited for domain '%s' (PID=%d), err=%v", dom, c.Process.Pid, err)
		p.PluginLogger.Writer().Close()
		p.mu.Lock()
		st.process = nil
		p.mu.Unlock()
	}(domain, cmd)
}

func (p *PythonPlugin) proxyToPython(domain string, w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	st := p.processes[domain]
	p.mu.Unlock()

	if st == nil {
		http.Error(w, "Python not configured for this domain", http.StatusBadGateway)
		return
	}

	targetURL := fmt.Sprintf("http://localhost:%s%s", st.config.Port, r.URL.Path)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	p.DomainLogger.Infof("[PythonPlugin] Delegating path=%s to Python", targetURL)

	bodyData, err := io.ReadAll(r.Body)
	if err != nil {
		p.PluginLogger.Errorf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	req, err := http.NewRequest(r.Method, targetURL, strings.NewReader(string(bodyData)))
	if err != nil {
		p.PluginLogger.Errorf("Failed to create request for Python app: %v", err)
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	for k, vals := range r.Header {
		for _, val := range vals {
			req.Header.Add(k, val)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		p.PluginLogger.Errorf("Failed to connect to Python backend [%s]: %v", domain, err)
		http.Error(w, "Python backend unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, val := range vals {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.PluginLogger.Errorf("Failed to read response from Python app [%s]: %v", domain, err)
		http.Error(w, "Failed to read response from Python app", http.StatusInternalServerError)
		return
	}
	w.Write(respBody)
}

func (p *PythonPlugin) setupVenv(cfg PythonPluginConfig, systemPython string) string {
	venvDir := filepath.Join(cfg.RootDir, ".venv")

	if _, err := os.Stat(venvDir); os.IsNotExist(err) {
		p.PluginLogger.Infof("Creating venv in %s", venvDir)
		cmd := exec.Command(systemPython, "-m", "venv", ".venv")
		cmd.Dir = cfg.RootDir
		cmd.Stdout = p.PluginLogger.Writer()
		cmd.Stderr = p.PluginLogger.Writer()
		if err := cmd.Run(); err != nil {
			p.PluginLogger.Errorf("Failed to create venv: %v", err)
			return ""
		}
	}

	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python")
}

func (p *PythonPlugin) installDeps(cfg PythonPluginConfig, pythonCmd string) {
	p.PluginLogger.Infof("Looking for dependencies in %s", cfg.RootDir)

	reqTxt := filepath.Join(cfg.RootDir, "requirements.txt")
	pyProj := filepath.Join(cfg.RootDir, "pyproject.toml")

	manager := cfg.PackageManager
	if manager == "" {
		manager = "pip"
	}

	plugin.ShowProgressBar("Installing Python dependencies...")
	defer plugin.HideProgressBar()

	switch manager {
	case "pip", "pip3":
		if _, err := os.Stat(reqTxt); err == nil {
			cmd := exec.Command(pythonCmd, "-m", "pip", "install", "-r", reqTxt)
			cmd.Dir = cfg.RootDir
			cmd.Stdout = p.PluginLogger.Writer()
			cmd.Stderr = p.PluginLogger.Writer()
			if err := cmd.Run(); err != nil {
				p.PluginLogger.Errorf("Failed to install deps with pip: %v", err)
			} else {
				fmt.Println("Python dependencies installed successfully (pip).")
			}
		} else {
			cmd := exec.Command(pythonCmd, "-m", "pip", "install")
			cmd.Dir = cfg.RootDir
			cmd.Stdout = p.PluginLogger.Writer()
			cmd.Stderr = p.PluginLogger.Writer()
			if err := cmd.Run(); err != nil {
				p.PluginLogger.Errorf("Failed to run pip install: %v", err)
			} else {
				fmt.Println("Python dependencies installed successfully (pip).")
			}
		}
	case "poetry":
		if _, err := os.Stat(pyProj); err == nil {
			cmd := exec.Command("poetry", "install")
			cmd.Dir = cfg.RootDir
			cmd.Stdout = p.PluginLogger.Writer()
			cmd.Stderr = p.PluginLogger.Writer()
			if err := cmd.Run(); err != nil {
				p.PluginLogger.Errorf("Failed to install deps with poetry: %v", err)
			} else {
				fmt.Println("Python dependencies installed successfully (poetry).")
			}
		}
	case "pipenv":
		cmd := exec.Command("pipenv", "install")
		cmd.Dir = cfg.RootDir
		cmd.Stdout = p.PluginLogger.Writer()
		cmd.Stderr = p.PluginLogger.Writer()
		if err := cmd.Run(); err != nil {
			p.PluginLogger.Errorf("Failed to install deps with pipenv: %v", err)
		} else {
			fmt.Println("Python dependencies installed successfully (pipenv).")
		}
	default:
		fmt.Printf("Package manager '%s' is not directly supported.\n", manager)
	}
}
