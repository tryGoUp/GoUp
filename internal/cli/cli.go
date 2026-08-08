package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mirkobrombin/go-cli-builder/v3/pkg/cli"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/pidfile"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/internal/restart"
	"github.com/mirkobrombin/goup/internal/server"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

// Build metadata, injected at release time via -ldflags -X.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Root flag values, mirrored from the parsed CLI root in Before so
// subcommands (which do not see the root fields) can use them.
var (
	configPath       string
	globalConfigPath string
)

// CLI is the root goup command.
type CLI struct {
	Config       string `cli:"config,c" help:"Path to specific site configuration file"`
	GlobalConfig string `cli:"global-config" help:"Path to specific global configuration file"`

	Generate GenerateCmd `cmd:"generate" help:"Generate a new configuration file for the specified domain"`
	GenPass  GenPassCmd  `cmd:"gen-pass" help:"Generate a Bcrypt hash for a password (prompts securely if omitted)"`
	Start    StartCmd    `cmd:"start" help:"Start the server"`
	StartWeb StartWebCmd `cmd:"start-web" help:"Start only the web server"`
	StartDNS StartDNSCmd `cmd:"start-dns" help:"Start only the DNS server"`
	Validate ValidateCmd `cmd:"validate" help:"Validate the configuration files"`
	List     ListCmd     `cmd:"list" help:"List all configured sites"`
	Plugins  PluginsCmd  `cmd:"plugins" help:"List all registered plugins"`
	Stop     StopCmd     `cmd:"stop" help:"Stop the running server"`
	Restart  RestartCmd  `cmd:"restart" help:"Restart the server"`
	Version  VersionCmd  `cmd:"version" help:"Print the GoUp version"`

	cli.Base
}

// Before mirrors the old PersistentPreRun: global config loads before any
// subcommand runs, and root flags are exposed to subcommands.
func (c *CLI) Before() error {
	configPath = c.Config
	globalConfigPath = c.GlobalConfig
	if err := config.LoadGlobalConfig(globalConfigPath); err != nil {
		return fmt.Errorf("error loading global config: %w", err)
	}
	return nil
}

// Execute parses os.Args and runs the selected command.
func Execute() {
	app := &CLI{}
	version := fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
	a, err := cli.New(app, cli.WithVersion(version))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	a.SetName("goup")
	if err := a.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// GenerateCmd generates a new site configuration interactively.
type GenerateCmd struct {
	Domain string `arg:"" required:"true" help:"Domain to generate the configuration for"`

	cli.Base
}

func (c *GenerateCmd) Run() error {
	domain := c.Domain
	configDir := config.GetConfigDir()
	os.MkdirAll(configDir, os.ModePerm)
	configFile := filepath.Join(configDir, domain+".json")

	if _, err := os.Stat(configFile); err == nil {
		return fmt.Errorf("configuration file for %s already exists", domain)
	}

	conf := config.SiteConfig{
		Domain: domain,
		SSL:    config.SSLConfig{},
	}

	reader := os.Stdin

	fmt.Printf("Configuration for %s:\n", domain)

	fmt.Print("Port [8080]: ")
	fmt.Fscanf(reader, "%d\n", &conf.Port)
	if conf.Port == 0 {
		conf.Port = 8080
	}

	defaultRoot := fmt.Sprintf("/var/www/%s", domain)
	fmt.Printf("Root directory [%s]: ", defaultRoot)
	fmt.Fscanf(reader, "%s\n", &conf.RootDirectory)
	if conf.RootDirectory == "" {
		conf.RootDirectory = defaultRoot
	}

	// Validating and resolving the root directory path to an absolute path
	// to avoid issues with relative paths.
	if !filepath.IsAbs(conf.RootDirectory) {
		absPath, err := filepath.Abs(conf.RootDirectory)
		if err != nil {
			return fmt.Errorf("error resolving root directory path: %w", err)
		}
		conf.RootDirectory = absPath
	}

	fmt.Print("Do you want to set up a proxy_pass? (y/N): ")
	var proxyAnswer string
	fmt.Fscanf(reader, "%s\n", &proxyAnswer)
	if strings.ToLower(proxyAnswer) == "y" {
		fmt.Print("Backend URL (e.g., http://localhost:3000): ")
		fmt.Fscanf(reader, "%s\n", &conf.ProxyPass)
	}

	conf.CustomHeaders = make(map[string]string)
	fmt.Print("Do you want to add custom headers? (y/N): ")
	var headersAnswer string
	fmt.Fscanf(reader, "%s\n", &headersAnswer)
	for strings.ToLower(headersAnswer) == "y" {
		var headerName, headerValue string
		fmt.Print("Header name: ")
		fmt.Fscanf(reader, "%s\n", &headerName)
		fmt.Print("Header value: ")
		fmt.Fscanf(reader, "%s\n", &headerValue)
		conf.CustomHeaders[headerName] = headerValue

		fmt.Print("Add another header? (y/N): ")
		fmt.Fscanf(reader, "%s\n", &headersAnswer)
	}

	fmt.Print("Do you want to enable SSL? (y/N): ")
	var sslAnswer string
	fmt.Fscanf(reader, "%s\n", &sslAnswer)
	if strings.ToLower(sslAnswer) == "y" {
		conf.SSL.Enabled = true
		fmt.Print("Path to SSL certificate: ")
		fmt.Fscanf(reader, "%s\n", &conf.SSL.Certificate)
		fmt.Print("Path to SSL key: ")
		fmt.Fscanf(reader, "%s\n", &conf.SSL.Key)
	}

	fmt.Print("Request timeout in seconds [60]: ")
	fmt.Fscanf(reader, "%d\n", &conf.RequestTimeout)
	if conf.RequestTimeout == 0 {
		conf.RequestTimeout = 60
	}

	if err := conf.Save(configFile); err != nil {
		return fmt.Errorf("error saving configuration: %w", err)
	}

	c.Logger.Success("Configuration file created at %s", configFile)
	return nil
}

// StartCmd starts web and DNS servers.
type StartCmd struct {
	TUI   bool `cli:"tui,t" help:"Enable TUI mode"`
	Bench bool `cli:"bench,b" help:"Enable benchmark mode"`

	cli.Base
}

func (c *StartCmd) Run() error {
	return startServers(c.TUI, c.Bench, server.ModeAll, "Starting full GoUp server (Web + DNS)...")
}

// StartWebCmd starts only the web server.
type StartWebCmd struct {
	cli.Base
}

func (c *StartWebCmd) Run() error {
	return startServers(false, false, server.ModeWeb, "Starting GoUp Web Server...")
}

// StartDNSCmd starts only the DNS server.
type StartDNSCmd struct {
	cli.Base
}

func (c *StartDNSCmd) Run() error {
	return startServers(false, false, server.ModeDNS, "Starting GoUp DNS Server...")
}

func startServers(tuiMode, benchMode bool, mode server.ServerMode, banner string) error {
	configs, err := loadConfigs()
	if err != nil {
		return fmt.Errorf("error loading configurations: %w", err)
	}

	if len(configs) == 0 {
		if configPath != "" {
			return fmt.Errorf("no valid configurations found in %s", configPath)
		}
		return fmt.Errorf("no configurations found in %s", config.GetConfigDir())
	}

	// Write PID file
	if err := pidfile.Write(); err != nil {
		fmt.Printf("Warning: could not write PID file: %v\n", err)
	}

	if !tuiMode {
		handleShutdownSignal()
	}

	fmt.Println(banner)
	server.StartServers(configs, tuiMode, benchMode, mode)

	if !tuiMode {
		select {}
	}
	return nil
}

func handleShutdownSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		if err := server.ShutdownServers(server.DefaultShutdownTimeout); err != nil {
			fmt.Printf("Error shutting down: %v\n", err)
		}
		pidfile.Remove()
		os.Exit(0)
	}()

	// SIGHUP reloads configuration: gracefully drain and re-exec, so config and
	// certificate changes are picked up without a manual stop/start.
	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)
	go func() {
		for range hupCh {
			fmt.Println("\nReloading configuration (SIGHUP)...")
			restart.Restart()
		}
	}()
}

func loadConfigs() ([]config.SiteConfig, error) {
	if configPath != "" {
		return config.LoadConfigsFromFile(configPath)
	}
	return config.LoadAllConfigs()
}

// StopCmd stops the running server.
type StopCmd struct {
	cli.Base
}

func (c *StopCmd) Run() error {
	pid, err := pidfile.Read()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("error finding process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("error sending signal: %w", err)
	}
	if waitForExit(pid, 10*time.Second) {
		c.Logger.Success("Server stopped.")
	} else {
		c.Logger.Warning("Server did not stop in time, forcing...")
		_ = proc.Kill()
		waitForExit(pid, 5*time.Second)
	}
	pidfile.Remove()
	return nil
}

// processAlive reports whether a process with the given PID is currently
// running. On Unix, signal 0 performs error checking without delivering a
// signal.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// waitForExit polls until the process is gone or the timeout elapses. It
// returns true if the process exited. This works for non-child processes,
// unlike (*os.Process).Wait.
func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !processAlive(pid)
}

// RestartCmd stops the running server and re-execs it as "start".
type RestartCmd struct {
	cli.Base
}

func (c *RestartCmd) Run() error {
	pid, err := pidfile.Read()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("error finding process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("error sending signal: %w", err)
	}
	// Wait for the old process to release its PID file, ports and
	// listeners before starting a fresh instance, otherwise the two
	// race over the PID file and the sockets.
	if !waitForExit(pid, 15*time.Second) {
		c.Logger.Warning("Previous instance did not stop in time, forcing...")
		_ = proc.Kill()
		waitForExit(pid, 5*time.Second)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// Re-exec as "start" (not "restart"), preserving the config flags so
	// the new process actually serves traffic instead of re-running the
	// restart command against a now-stopped server.
	newArgs := []string{exe, "start"}
	if configPath != "" {
		newArgs = append(newArgs, "--config", configPath)
	}
	if globalConfigPath != "" {
		newArgs = append(newArgs, "--global-config", globalConfigPath)
	}
	if err := syscall.Exec(exe, newArgs, os.Environ()); err != nil {
		return fmt.Errorf("error restarting: %w", err)
	}
	return nil
}

// ValidateCmd validates all configuration files.
type ValidateCmd struct {
	cli.Base
}

func (c *ValidateCmd) Run() error {
	configDir := config.GetConfigDir()
	fmt.Printf("Validating configurations in %s:\n", configDir)

	fileErrors, crossErrors, err := config.ValidateAll()
	if err != nil {
		return fmt.Errorf("error reading configuration directory %s: %w", configDir, err)
	}

	// Report each file. ValidateAll only returns entries with problems, so read
	// the directory to also print the OK ones.
	files, _ := os.ReadDir(configDir)
	hasError := len(crossErrors) > 0
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || filepath.Ext(name) != ".json" || name == "conf.global.json" {
			continue
		}
		if problems, bad := fileErrors[name]; bad {
			hasError = true
			fmt.Printf("- %s: FAILED\n", name)
			for _, p := range problems {
				fmt.Printf("    - %s\n", p)
			}
		} else {
			fmt.Printf("- %s: OK\n", name)
		}
	}

	for _, ce := range crossErrors {
		fmt.Printf("! %s\n", ce)
	}

	if hasError {
		return fmt.Errorf("configuration validation failed")
	}
	return nil
}

// ListCmd lists all configured sites.
type ListCmd struct {
	cli.Base
}

func (c *ListCmd) Run() error {
	configs, err := config.LoadAllConfigs()
	if err != nil {
		return fmt.Errorf("error loading configurations: %w", err)
	}

	fmt.Println("Configured sites:")
	for _, conf := range configs {
		fmt.Printf("- %s (port %d)\n", conf.Domain, conf.Port)
	}
	return nil
}

// PluginsCmd lists all registered plugins.
type PluginsCmd struct {
	cli.Base
}

func (c *PluginsCmd) Run() error {
	pluginManager := plugin.GetPluginManagerInstance()
	plugins := pluginManager.GetRegisteredPlugins()

	if len(plugins) == 0 {
		fmt.Println("No plugins registered.")
		return nil
	}

	fmt.Println("Registered plugins:")
	for _, name := range plugins {
		fmt.Printf("- %s\n", name)
	}
	return nil
}

// VersionCmd prints version, commit and build date.
type VersionCmd struct {
	cli.Base
}

func (c *VersionCmd) Run() error {
	fmt.Printf("goup %s\ncommit: %s\nbuilt:  %s\n", Version, Commit, Date)
	return nil
}

// GenPassCmd generates a Bcrypt password hash.
type GenPassCmd struct {
	Password string `arg:"" help:"Password to hash (prompted securely if omitted)"`

	cli.Base
}

func (c *GenPassCmd) Run() error {
	var password []byte
	var err error

	if c.Password != "" {
		password = []byte(c.Password)
	} else {
		fmt.Print("Enter Password: ")
		password, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println() // Add newline after silent input
		if err != nil {
			return fmt.Errorf("error reading password: %w", err)
		}
	}

	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("error generating hash: %w", err)
	}

	fmt.Println(string(hash))
	return nil
}
