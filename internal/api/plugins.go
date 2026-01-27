package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/internal/restart"
)

type PluginResponse struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func getPluginsHandler(w http.ResponseWriter, r *http.Request) {
	pm := plugin.GetPluginManagerInstance()
	all := pm.GetRegisteredPlugins()
	var out []PluginResponse
	for _, name := range all {
		out = append(out, PluginResponse{
			Name:    name,
			Enabled: isPluginEnabled(name),
		})
	}
	jsonResponse(w, out)
}

func togglePluginHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pName := vars["pluginName"]

	if config.GlobalConf == nil {
		http.Error(w, "Global config not loaded", http.StatusInternalServerError)
		return
	}
	idx := -1
	for i, n := range config.GlobalConf.EnabledPlugins {
		if n == pName {
			idx = i
			break
		}
	}
	if idx >= 0 {
		config.GlobalConf.EnabledPlugins = append(
			config.GlobalConf.EnabledPlugins[:idx],
			config.GlobalConf.EnabledPlugins[idx+1:]...,
		)
	} else {
		config.GlobalConf.EnabledPlugins = append(config.GlobalConf.EnabledPlugins, pName)
	}
	if err := config.SaveGlobalConfig(); err != nil {
		http.Error(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]any{
		"name":    pName,
		"enabled": isPluginEnabled(pName),
	})

	restart.ScheduleRestart(5)
}

func isPluginEnabled(name string) bool {
	if config.GlobalConf == nil || len(config.GlobalConf.EnabledPlugins) == 0 {
		return true
	}
	for _, p := range config.GlobalConf.EnabledPlugins {
		if p == name {
			return true
		}
	}
	return false
}
