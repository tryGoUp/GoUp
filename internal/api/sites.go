package api

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"

	"github.com/gorilla/mux"
	"github.com/mirkobrombin/goup/internal/config"
)

func SitesSnapshot() []config.SiteConfig {
	var all []config.SiteConfig
	config.SiteConfigsMu.RLock()
	defer config.SiteConfigsMu.RUnlock()
	for _, site := range config.SiteConfigs {
		all = append(all, site)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Domain < all[j].Domain
	})
	return all
}

func listSitesHandler(w http.ResponseWriter, r *http.Request) {
	all := SitesSnapshot()
	jsonResponse(w, all)
}

func getSiteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domain := vars["domain"]
	config.SiteConfigsMu.RLock()
	site, ok := config.SiteConfigs[domain]
	config.SiteConfigsMu.RUnlock()
	if !ok {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, site)
}

func createSiteHandler(w http.ResponseWriter, r *http.Request) {
	var site config.SiteConfig
	if err := json.NewDecoder(r.Body).Decode(&site); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	path, err := config.SiteConfigPath(site.Domain)
	if err != nil {
		http.Error(w, "Invalid domain", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(path); err == nil {
		http.Error(w, "Site already exists", http.StatusBadRequest)
		return
	}
	if err := site.Save(path); err != nil {
		http.Error(w, "Failed to save site config", http.StatusInternalServerError)
		return
	}
	config.SiteConfigsMu.Lock()
	config.SiteConfigs[site.Domain] = site
	config.SiteConfigsMu.Unlock()
	jsonResponse(w, site)
}

func updateSiteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domain := vars["domain"]
	config.SiteConfigsMu.RLock()
	existing, ok := config.SiteConfigs[domain]
	config.SiteConfigsMu.RUnlock()
	if !ok {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}
	var updated config.SiteConfig
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	existing.Port = updated.Port
	existing.RootDirectory = updated.RootDirectory
	existing.CustomHeaders = updated.CustomHeaders
	existing.ProxyPass = updated.ProxyPass
	existing.SSL = updated.SSL
	existing.RequestTimeout = updated.RequestTimeout
	existing.PluginConfigs = updated.PluginConfigs

	path, err := config.SiteConfigPath(domain)
	if err != nil {
		http.Error(w, "Invalid domain", http.StatusBadRequest)
		return
	}
	if err := existing.Save(path); err != nil {
		http.Error(w, "Failed to save site config", http.StatusInternalServerError)
		return
	}
	config.SiteConfigsMu.Lock()
	config.SiteConfigs[domain] = existing
	config.SiteConfigsMu.Unlock()
	jsonResponse(w, existing)
}

func deleteSiteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domain := vars["domain"]
	path, err := config.SiteConfigPath(domain)
	if err != nil {
		http.Error(w, "Invalid domain", http.StatusBadRequest)
		return
	}
	if err := os.Remove(path); err != nil {
		http.Error(w, "Failed to delete site config", http.StatusInternalServerError)
		return
	}
	config.SiteConfigsMu.Lock()
	delete(config.SiteConfigs, domain)
	config.SiteConfigsMu.Unlock()
	jsonResponse(w, map[string]string{"deleted": domain})
}

func validateSiteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domain := vars["domain"]
	config.SiteConfigsMu.RLock()
	site, ok := config.SiteConfigs[domain]
	config.SiteConfigsMu.RUnlock()
	if !ok {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}
	errs := site.Validate()
	if len(errs) > 0 {
		jsonResponse(w, map[string]any{
			"valid":  false,
			"errors": errs,
		})
		return
	}
	jsonResponse(w, map[string]any{"valid": true})
}
