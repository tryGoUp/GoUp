package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// customLogDir is used to override the default log directory, e.g. for testing.
var customLogDir string

// SiteConfigs is a global map of site configurations keyed by domain.
var SiteConfigs = make(map[string]SiteConfig)

// SSLConfig represents the SSL configuration for a site.
type SSLConfig struct {
	Enabled     bool   `json:"enabled"`
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

// SiteConfig contains the configuration for a single site.
type SiteConfig struct {
	Domain                   string            `json:"domain"`
	Port                     int               `json:"port"`
	RootDirectory            string            `json:"root_directory"`
	CustomHeaders            map[string]string `json:"custom_headers"`
	ProxyPass                string            `json:"proxy_pass"`
	SSL                      SSLConfig         `json:"ssl"`
	RequestTimeout           int               `json:"request_timeout"`     // in seconds
	ReadHeaderTimeout        int               `json:"read_header_timeout"` // in seconds
	IdleTimeout              int               `json:"idle_timeout"`        // in seconds
	MaxHeaderBytes           int               `json:"max_header_bytes"`    // in bytes
	FlushInterval            string            `json:"proxy_flush_interval"`
	BufferSizeKB             int               `json:"buffer_size_kb"`
	MaxConcurrentConnections int               `json:"max_concurrent_connections"`
	EnableLogging            *bool             `json:"enable_logging,omitempty"` // Default true if nil
	FileServerMode           bool              `json:"file_server_mode"`         // Disables custom pages, enables directory listing

	PluginConfigs map[string]any `json:"plugin_configs"`
}

// GetConfigDir returns the directory where configuration files are stored.
func GetConfigDir() string {
	var configDir string
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		configDir = filepath.Join(xdgConfig, "goup")
	} else if runtime.GOOS == "windows" {
		configDir = filepath.Join(os.Getenv("APPDATA"), "goup")
	} else {
		configDir = filepath.Join(os.Getenv("HOME"), ".config", "goup")
	}
	return configDir
}

// GetLogDir returns the directory where log files are stored.
func GetLogDir() string {
	if customLogDir != "" {
		return customLogDir
	}

	var logDir string
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		logDir = filepath.Join(xdgDataHome, "goup", "logs")
	} else if runtime.GOOS == "windows" {
		logDir = filepath.Join(os.Getenv("APPDATA"), "goup", "logs")
	} else {
		logDir = filepath.Join(os.Getenv("HOME"), ".local", "share", "goup", "logs")
	}
	return logDir
}

// LoadConfig loads a configuration from a file.
func LoadConfig(filePath string) (SiteConfig, error) {
	var conf SiteConfig
	data, err := os.ReadFile(filePath)
	if err != nil {
		return conf, err
	}
	if err := json.Unmarshal(data, &conf); err != nil {
		return conf, err
	}
	if conf.PluginConfigs == nil {
		conf.PluginConfigs = make(map[string]any)
	}
	return conf, nil
}

// LoadConfigsFromFile loads configurations from a file, supporting both single object and array.
func LoadConfigsFromFile(filePath string) ([]SiteConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var configs []SiteConfig

	// Try to unmarshal as array
	if err := json.Unmarshal(data, &configs); err == nil {
		// Ensure plugin configs map is initialized for each
		for i := range configs {
			if configs[i].PluginConfigs == nil {
				configs[i].PluginConfigs = make(map[string]any)
			}
		}
		return configs, nil
	}

	// Try to unmarshal as single object
	var conf SiteConfig
	if err := json.Unmarshal(data, &conf); err == nil {
		if conf.PluginConfigs == nil {
			conf.PluginConfigs = make(map[string]any)
		}
		return []SiteConfig{conf}, nil
	}

	return nil, fmt.Errorf("failed to parse config file: %s (must be SiteConfig or []SiteConfig local json)", filePath)
}

// LoadAllConfigs loads all configurations from the configuration directory.
func LoadAllConfigs() ([]SiteConfig, error) {
	configDir := GetConfigDir()
	files, err := os.ReadDir(configDir)
	if err != nil {
		return nil, err
	}

	var configs []SiteConfig
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".json" {
			wg.Add(1)
			go func(fname string) {
				defer wg.Done()

				conf, err := LoadConfig(filepath.Join(configDir, fname))
				if err != nil {
					fmt.Printf("Error loading config %s: %v\n", fname, err)
					return
				}

				mu.Lock()
				configs = append(configs, conf)
				SiteConfigs[conf.Domain] = conf
				mu.Unlock()
			}(file.Name())
		}
	}

	wg.Wait()
	return configs, nil
}

// Save saves the configuration to a file.
func (conf *SiteConfig) Save(filePath string) error {
	data, err := json.MarshalIndent(conf, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// GetSiteConfigByHost returns the site configuration based on the host.
func GetSiteConfigByHost(host string) (SiteConfig, error) {
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	if conf, ok := SiteConfigs[host]; ok {
		return conf, nil
	}
	return SiteConfig{}, fmt.Errorf("site configuration not found for host: %s", host)
}

// SetCustomLogDir allows setting a custom log directory for testing.
func SetCustomLogDir(dir string) {
	customLogDir = dir
}
