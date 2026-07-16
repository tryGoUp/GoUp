package api

import (
	"encoding/json"
	"net/http"

	"github.com/mirkobrombin/goup/internal/config"
)

func getConfigHandler(w http.ResponseWriter, r *http.Request) {
	config.GlobalConfMu.RLock()
	defer config.GlobalConfMu.RUnlock()
	if config.GlobalConf == nil {
		http.Error(w, "Global config not loaded", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, config.GlobalConf)
}

func updateConfigHandler(w http.ResponseWriter, r *http.Request) {
	config.GlobalConfMu.RLock()
	if config.GlobalConf == nil {
		config.GlobalConfMu.RUnlock()
		http.Error(w, "Global config not loaded", http.StatusInternalServerError)
		return
	}
	config.GlobalConfMu.RUnlock()
	var newConf config.GlobalConfig
	if err := json.NewDecoder(r.Body).Decode(&newConf); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Reject changes that would leave an admin surface enabled without
	// credentials (which would otherwise disable authentication entirely).
	if newConf.EnableAPI && newConf.Account.APIToken == "" {
		http.Error(w, "Refusing to enable the API without account.api_token", http.StatusBadRequest)
		return
	}
	if newConf.DashboardPort != 0 && (newConf.Account.Username == "" || newConf.Account.PasswordHash == "") {
		http.Error(w, "Refusing to enable the dashboard without account.username and account.password_hash", http.StatusBadRequest)
		return
	}

	config.GlobalConfMu.Lock()
	config.GlobalConf = &newConf
	config.GlobalConfMu.Unlock()
	if err := config.SaveGlobalConfig(); err != nil {
		http.Error(w, "Failed to save config", http.StatusInternalServerError)
		return
	}
	config.GlobalConfMu.RLock()
	defer config.GlobalConfMu.RUnlock()
	jsonResponse(w, config.GlobalConf)
}
