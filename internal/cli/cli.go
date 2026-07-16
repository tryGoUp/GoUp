package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/pidfile"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/internal/restart"
	"github.com/mirkobrombin/goup/internal/server"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

var tuiMode bool
var benchMode bool
var configPath string
var globalConfigPath string

// Build metadata, injected at release time via -ldflags -X.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:     "goup",
	Short:   "GoUP is a minimal configurable web server",
	Long:    `GoUP is a minimal configurable web server written in Go.`,
	Version: fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if err := config.LoadGlobalConfig(globalConfigPath); err != nil {
			fmt.Printf("Error loading global config: %v\n", err)
			os.Exit(1)
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(genPassCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(startWebCmd)
	rootCmd.AddCommand(startDNSCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(pluginsCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(versionCmd)

	startCmd.Flags().BoolVarP(&tuiMode, "tui", "t", false, "Enable TUI mode")
	startCmd.Flags().BoolVarP(&benchMode, "bench", "b", false, "Enable benchmark mode")

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to specific site configuration file")
	rootCmd.PersistentFlags().StringVar(&globalConfigPath, "global-config", "", "Path to specific global configuration file")
}

var generateCmd = &cobra.Command{
	Use:   "generate [domain]",
	Short: "Generate a new configuration file for the specified domain",
	Args:  cobra.ExactArgs(1),
	Run:   generate,
}

func generate(cmd *cobra.Command, args []string) {
	domain := args[0]
	configDir := config.GetConfigDir()
	os.MkdirAll(configDir, os.ModePerm)
	configFile := filepath.Join(configDir, domain+".json")

	if _, err := os.Stat(configFile); err == nil {
		fmt.Printf("Configuration file for %s already exists.\n", domain)
		os.Exit(1)
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
			fmt.Printf("Error resolving root directory path: %v\n", err)
			os.Exit(1)
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
		fmt.Printf("Error saving configuration: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Configuration file created at %s\n", configFile)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the server",
	Run:   start,
}

func start(cmd *cobra.Command, args []string) {
	configs, err := loadConfigs()
	if err != nil {
		fmt.Printf("Error loading configurations: %v\n", err)
		os.Exit(1)
	}

	if len(configs) == 0 {
		if configPath != "" {
			fmt.Printf("No valid configurations found in %s\n", configPath)
		} else {
			fmt.Printf("No configurations found in %s\n", config.GetConfigDir())
		}
		os.Exit(1)
	}

	// Write PID file
	if err := pidfile.Write(); err != nil {
		fmt.Printf("Warning: could not write PID file: %v\n", err)
	}

	if !tuiMode {
		handleShutdownSignal()
	}

	fmt.Println("Starting full GoUp server (Web + DNS)...")
	server.StartServers(configs, tuiMode, benchMode, server.ModeAll)

	if !tuiMode {
		select {}
	}
}

var startWebCmd = &cobra.Command{
	Use:   "start-web",
	Short: "Start only the web server",
	Run:   startWeb,
}

func startWeb(cmd *cobra.Command, args []string) {
	configs, err := loadConfigs()
	if err != nil {
		fmt.Printf("Error loading configurations: %v\n", err)
		os.Exit(1)
	}

	if err := pidfile.Write(); err != nil {
		fmt.Printf("Warning: could not write PID file: %v\n", err)
	}

	if !tuiMode {
		handleShutdownSignal()
	}

	fmt.Println("Starting GoUp Web Server...")
	server.StartServers(configs, tuiMode, benchMode, server.ModeWeb)

	if !tuiMode {
		select {}
	}
}

var startDNSCmd = &cobra.Command{
	Use:   "start-dns",
	Short: "Start only the DNS server",
	Run:   startDNS,
}

func startDNS(cmd *cobra.Command, args []string) {
	configs, err := loadConfigs()
	if err != nil {
		fmt.Printf("Error loading configurations: %v\n", err)
		os.Exit(1)
	}

	if err := pidfile.Write(); err != nil {
		fmt.Printf("Warning: could not write PID file: %v\n", err)
	}

	if !tuiMode {
		handleShutdownSignal()
	}

	fmt.Println("Starting GoUp DNS Server...")
	server.StartServers(configs, tuiMode, benchMode, server.ModeDNS)

	if !tuiMode {
		select {}
	}
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

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running server",
	Run: func(cmd *cobra.Command, args []string) {
		pid, err := pidfile.Read()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			fmt.Printf("Error finding process %d: %v\n", pid, err)
			os.Exit(1)
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			fmt.Printf("Error sending signal: %v\n", err)
			os.Exit(1)
		}
		if waitForExit(pid, 10*time.Second) {
			fmt.Println("Server stopped.")
		} else {
			fmt.Println("Server did not stop in time, forcing...")
			_ = proc.Kill()
			waitForExit(pid, 5*time.Second)
		}
		pidfile.Remove()
	},
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

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the server",
	Run: func(cmd *cobra.Command, args []string) {
		pid, err := pidfile.Read()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			fmt.Printf("Error finding process %d: %v\n", pid, err)
			os.Exit(1)
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			fmt.Printf("Error sending signal: %v\n", err)
			os.Exit(1)
		}
		// Wait for the old process to release its PID file, ports and
		// listeners before starting a fresh instance, otherwise the two
		// race over the PID file and the sockets.
		if !waitForExit(pid, 15*time.Second) {
			fmt.Println("Previous instance did not stop in time, forcing...")
			_ = proc.Kill()
			waitForExit(pid, 5*time.Second)
		}

		exe, err := os.Executable()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
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
			fmt.Printf("Error restarting: %v\n", err)
			os.Exit(1)
		}
	},
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the configuration files",
	Run:   validate,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the GoUp version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("goup %s\ncommit: %s\nbuilt:  %s\n", Version, Commit, Date)
	},
}

func validate(cmd *cobra.Command, args []string) {
	configDir := config.GetConfigDir()
	fmt.Printf("Validating configurations in %s:\n", configDir)

	fileErrors, crossErrors, err := config.ValidateAll()
	if err != nil {
		fmt.Printf("Error reading configuration directory %s: %v\n", configDir, err)
		os.Exit(1)
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

	for _, c := range crossErrors {
		fmt.Printf("! %s\n", c)
	}

	if hasError {
		os.Exit(1)
	}
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured sites",
	Run:   list,
}

func list(cmd *cobra.Command, args []string) {
	configs, err := config.LoadAllConfigs()
	if err != nil {
		fmt.Printf("Error loading configurations: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Configured sites:")
	for _, conf := range configs {
		fmt.Printf("- %s (port %d)\n", conf.Domain, conf.Port)
	}
}

var pluginsCmd = &cobra.Command{
	Use:   "plugins",
	Short: "List all registered plugins",
	Run: func(cmd *cobra.Command, args []string) {
		pluginManager := plugin.GetPluginManagerInstance()
		plugins := pluginManager.GetRegisteredPlugins()

		if len(plugins) == 0 {
			fmt.Println("No plugins registered.")
			return
		}

		fmt.Println("Registered plugins:")
		for _, name := range plugins {
			fmt.Printf("- %s\n", name)
		}
	},
}

// genPassCmd generates a Bcrypt password hash.
var genPassCmd = &cobra.Command{
	Use:   "gen-pass [password]",
	Short: "Generate a Bcrypt hash for a password",
	Long:  `Generate a Bcrypt hash for a password. If no password is provided as an argument, you will be prompted to enter one securely.`,
	Args:  cobra.MaximumNArgs(1),
	Run:   genPass,
}

func genPass(cmd *cobra.Command, args []string) {
	var password []byte
	var err error

	if len(args) > 0 {
		password = []byte(args[0])
	} else {
		fmt.Print("Enter Password: ")
		password, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println() // Add newline after silent input
		if err != nil {
			fmt.Printf("Error reading password: %v\n", err)
			os.Exit(1)
		}
	}

	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("Error generating hash: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(hash))
}
