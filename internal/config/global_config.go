package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SafeGuardConfig defines configuration for the SafeGuard background routine.
type SafeGuardConfig struct {
	Enable        *bool  `json:"enable"`         // Explicitly enable/disable (default true)
	MaxMemoryMB   int    `json:"max_memory_mb"`  // Restart if RSS > this (default 1024)
	CheckInterval string `json:"check_interval"` // Interval to check memory (e.g. "10s")
}

// AccountConfig defines the authentication credentials.
type AccountConfig struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"` // Bcrypt hash
	APIToken     string `json:"api_token"`
}

// GlobalConfig contains the global settings for GoUP.
type GlobalConfig struct {
	Account        AccountConfig   `json:"account"`
	EnableAPI      bool            `json:"enable_api"`
	APIPort        int             `json:"api_port"`
	DashboardPort  int             `json:"dashboard_port"`
	EnabledPlugins []string        `json:"enabled_plugins"` // empty means all enabled
	SafeGuard      SafeGuardConfig `json:"safeguard"`
}

// GlobalConf is the global configuration in memory.
var GlobalConf *GlobalConfig
var globalConfName = "conf.global.json"

// LoadGlobalConfig loads the global configuration file.
func LoadGlobalConfig() error {
	configDir := GetConfigDir()
	configFile := filepath.Join(configDir, globalConfName)
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		GlobalConf = &GlobalConfig{
			EnableAPI:      true,
			APIPort:        6007,
			DashboardPort:  6008,
			EnabledPlugins: []string{},
		}
		return nil
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}
	var conf GlobalConfig
	if err := json.Unmarshal(data, &conf); err != nil {
		return err
	}
	GlobalConf = &conf
	return nil
}

// SaveGlobalConfig saves the global configuration file.
func SaveGlobalConfig() error {
	configDir := GetConfigDir()
	configFile := filepath.Join(configDir, globalConfName)
	data, err := json.MarshalIndent(GlobalConf, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0644)
}
