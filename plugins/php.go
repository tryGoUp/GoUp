package plugins

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/yookoala/gofast"
)

// PHPPluginConfig represents the configuration for the PHPPlugin.
type PHPPluginConfig struct {
	Enable  bool   `json:"enable"`
	FPMAddr string `json:"fpm_addr"`
}

type PHPPlugin struct {
	plugin.BasePlugin
	siteConfigs map[string]PHPPluginConfig
}

func (p *PHPPlugin) Name() string {
	return "PHPPlugin"
}

func (p *PHPPlugin) OnInit() error {
	p.siteConfigs = make(map[string]PHPPluginConfig)
	return nil
}

func (p *PHPPlugin) OnInitForSite(conf config.SiteConfig, domainLogger *logger.Logger) error {
	if err := p.SetupLoggers(conf, p.Name(), domainLogger); err != nil {
		return err
	}

	// Retrieve site-specific plugin config
	pluginConfigRaw, ok := conf.PluginConfigs[p.Name()]
	if !ok {
		// No config for PHP, store default disabled config.
		p.siteConfigs[conf.Domain] = PHPPluginConfig{}
		return nil
	}

	cfg := PHPPluginConfig{}
	if rawMap, ok := pluginConfigRaw.(map[string]any); ok {
		// Use BasePlugin's IsEnabled method to determine if the plugin is enabled.
		cfg.Enable = p.IsEnabled(rawMap)
		if fpmAddr, ok := rawMap["fpm_addr"].(string); ok {
			cfg.FPMAddr = fpmAddr
		}
	}
	p.siteConfigs[conf.Domain] = cfg

	return nil
}

func (p *PHPPlugin) BeforeRequest(r *http.Request) {}

func (p *PHPPlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	cfg, ok := p.siteConfigs[host]
	if !ok || !cfg.Enable {
		return false
	}

	// We only handle .php files.
	if !strings.HasSuffix(r.URL.Path, ".php") {
		return false
	}

	p.DomainLogger.Infof("[PHPPlugin] Handling PHP request: %s (domain=%s)", r.URL.Path, host)

	// If the user hasn't specified a FPM address, use default.
	phpFPMAddr := cfg.FPMAddr
	if phpFPMAddr == "" {
		phpFPMAddr = "127.0.0.1:9000"
	}

	scriptFilename := filepath.Join(".", r.URL.Path)
	if _, err := os.Stat(scriptFilename); os.IsNotExist(err) {
		http.NotFound(w, r)
		return true
	}

	var connFactory gofast.ConnFactory
	if strings.HasPrefix(phpFPMAddr, "/") {
		connFactory = gofast.SimpleConnFactory("unix", phpFPMAddr)
	} else {
		connFactory = gofast.SimpleConnFactory("tcp", phpFPMAddr)
	}

	clientFactory := gofast.SimpleClientFactory(connFactory)

	fcgiHandler := gofast.NewHandler(
		func(client gofast.Client, req *gofast.Request) (*gofast.ResponsePipe, error) {
			req.Params["SCRIPT_FILENAME"] = scriptFilename
			req.Params["DOCUMENT_ROOT"] = "."
			req.Params["REQUEST_METHOD"] = r.Method
			req.Params["SERVER_PROTOCOL"] = r.Proto
			req.Params["REQUEST_URI"] = r.URL.RequestURI()
			req.Params["QUERY_STRING"] = r.URL.RawQuery
			req.Params["REMOTE_ADDR"] = r.RemoteAddr
			return gofast.BasicSession(client, req)
		},
		clientFactory,
	)

	fcgiHandler.ServeHTTP(w, r)
	return true
}

func (p *PHPPlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}
func (p *PHPPlugin) OnExit() error                                       { return nil }
