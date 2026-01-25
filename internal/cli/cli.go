package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/internal/server"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

var tuiMode bool
var benchMode bool
var configPath string
var globalConfigPath string

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "goup",
	Short: "GoUP is a minimal configurable web server",
	Long:  `GoUP is a minimal configurable web server written in Go.`,
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

	fmt.Println("Starting full GoUp server (Web + DNS)...")
	server.StartServers(configs, tuiMode, benchMode, server.ModeAll)

	// Wait indefinitely if not in TUI mode, the servers will keep running
	// and loggers will keep writing to both the stdout and the log files.
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
	// We might not strictly need site configs for DNS only, but StartServers expects them
	// effectively ignoring them if ModeWeb is not set, except for context setup.
	configs, _ := loadConfigs()

	fmt.Println("Starting GoUp DNS Server...")
	server.StartServers(configs, tuiMode, benchMode, server.ModeDNS)

	if !tuiMode {
		select {}
	}
}

func loadConfigs() ([]config.SiteConfig, error) {
	if configPath != "" {
		return config.LoadConfigsFromFile(configPath)
	}
	return config.LoadAllConfigs()
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the configuration files",
	Run:   validate,
}

func validate(cmd *cobra.Command, args []string) {
	configs, err := config.LoadAllConfigs()
	if err != nil {
		fmt.Printf("Error loading configurations: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Validating configurations in %s:\n", config.GetConfigDir())
	for _, conf := range configs {
		fmt.Printf("- %s: OK\n", conf.Domain)
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
