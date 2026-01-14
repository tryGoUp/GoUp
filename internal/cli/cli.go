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
)

var tuiMode bool
var benchMode bool
var configPath string

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "goup",
	Short: "GoUP is a minimal configurable web server",
	Long:  `GoUP is a minimal configurable web server written in Go.`,
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
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(pluginsCmd)

	startCmd.Flags().BoolVarP(&tuiMode, "tui", "t", false, "Enable TUI mode")
	startCmd.Flags().BoolVarP(&benchMode, "bench", "b", false, "Enable benchmark mode")
	startCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to specific configuration file")
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
	var configs []config.SiteConfig
	var err error

	if configPath != "" {
		configs, err = config.LoadConfigsFromFile(configPath)
		if err != nil {
			fmt.Printf("Error loading config from %s: %v\n", configPath, err)
			os.Exit(1)
		}
	} else {
		configs, err = config.LoadAllConfigs()
		if err != nil {
			fmt.Printf("Error loading configurations: %v\n", err)
			os.Exit(1)
		}
	}

	if len(configs) == 0 {
		if configPath != "" {
			fmt.Printf("No valid configurations found in %s\n", configPath)
		} else {
			fmt.Printf("No configurations found in %s\n", config.GetConfigDir())
		}
		os.Exit(1)
	}

	fmt.Println("Starting servers...")
	server.StartServers(configs, tuiMode, benchMode)

	// Wait indefinitely if not in TUI mode, the servers will keep running
	// and loggers will keep writing to both the stdout and the log files.
	if !tuiMode {
		select {}
	}
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
