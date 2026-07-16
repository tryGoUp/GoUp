package plugins

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/yookoala/gofast"
)

// PHPPluginConfig represents the configuration for the PHPPlugin.
type PHPPluginConfig struct {
	Enable  bool   `json:"enable"`
	FPMAddr string `json:"fpm_addr"`
	// FrontController is the script that handles requests which do not map to
	// an existing file, relative to the site root. This is what WordPress and
	// other front-controller apps need to serve pretty permalinks
	// (e.g. /iscrizione/ -> /index.php). Defaults to "index.php".
	FrontController string `json:"front_controller"`
	RootDir         string `json:"-"`
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
		if fc, ok := rawMap["front_controller"].(string); ok {
			cfg.FrontController = fc
		}
	}
	// Resolve the document root once so SCRIPT_FILENAME can be absolute.
	if abs, err := filepath.Abs(conf.RootDirectory); err == nil {
		cfg.RootDir = abs
	} else {
		cfg.RootDir = conf.RootDirectory
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

	docRoot := cfg.RootDir
	if docRoot == "" {
		docRoot = "."
	}

	// Resolve the requested path under the document root. Backslashes are
	// normalized first so Windows-style separators cannot bypass the clean,
	// and anchoring the clean to "/" prevents path traversal from escaping
	// the root.
	cleanPath := path.Clean("/" + strings.ReplaceAll(r.URL.Path, "\\", "/"))
	fsPath, err := phpScriptPath(docRoot, cleanPath)
	if err != nil {
		return false
	}

	var scriptFilename, scriptName, pathInfo string

	info, statErr := os.Stat(fsPath)
	switch {
	case statErr == nil && info.IsDir():
		// Directory request: serve its index.php when present.
		idxPath := filepath.Join(fsPath, "index.php")
		if fi, err := os.Stat(idxPath); err == nil && !fi.IsDir() {
			scriptFilename = idxPath
			scriptName = strings.TrimSuffix(cleanPath, "/") + "/index.php"
		}

	case statErr == nil:
		// Existing file: execute it if it is PHP, otherwise let the static
		// file server handle it (assets, css, js, images, ...).
		if !strings.HasSuffix(cleanPath, ".php") {
			return false
		}
		scriptFilename = fsPath
		scriptName = cleanPath
	}

	if scriptFilename == "" {
		// The path does not exist (or is a directory without index.php):
		// route the request through the front controller so the app can
		// handle pretty URLs (e.g. WordPress permalinks).
		front := cfg.FrontController
		if front == "" {
			front = "index.php"
		}
		frontClean := path.Clean("/" + strings.ReplaceAll(front, "\\", "/"))
		frontPath, err := phpScriptPath(docRoot, frontClean)
		if err != nil {
			return false
		}
		fi, err := os.Stat(frontPath)
		if err != nil || fi.IsDir() {
			// No front controller available: fall through to the static
			// handler, which will render the 404.
			return false
		}
		scriptFilename = frontPath
		scriptName = frontClean
		// Preserve the original URI as PATH_INFO so the app sees the route.
		pathInfo = r.URL.Path
	}

	p.DomainLogger.Infof("[PHPPlugin] Handling PHP request: %s -> %s (domain=%s)", r.URL.Path, scriptFilename, host)

	// If the user hasn't specified a FPM address, use default.
	phpFPMAddr := cfg.FPMAddr
	if phpFPMAddr == "" {
		phpFPMAddr = "127.0.0.1:9000"
	}

	if strings.HasPrefix(phpFPMAddr, "/") && runtime.GOOS == "windows" {
		http.Error(w, "Unix sockets are not supported on Windows", http.StatusInternalServerError)
		return true
	}

	clientFactory := fpmClientFactory(phpFPMAddr)

	fcgiHandler := gofast.NewHandler(
		func(client gofast.Client, req *gofast.Request) (*gofast.ResponsePipe, error) {
			req.Params["SCRIPT_FILENAME"] = scriptFilename
			req.Params["SCRIPT_NAME"] = scriptName
			req.Params["DOCUMENT_ROOT"] = docRoot
			if pathInfo != "" {
				req.Params["PATH_INFO"] = pathInfo
				req.Params["PATH_TRANSLATED"] = fsPath
			}
			req.Params["REQUEST_METHOD"] = r.Method
			req.Params["SERVER_PROTOCOL"] = r.Proto
			req.Params["REQUEST_URI"] = r.URL.RequestURI()
			req.Params["QUERY_STRING"] = r.URL.RawQuery
			req.Params["REMOTE_ADDR"] = r.RemoteAddr
			// Pass host info so the app can build absolute URLs / redirects.
			req.Params["HTTP_HOST"] = r.Host
			serverName := r.Host
			serverPort := ""
			if i := strings.LastIndex(r.Host, ":"); i != -1 {
				serverName = r.Host[:i]
				serverPort = r.Host[i+1:]
			}
			req.Params["SERVER_NAME"] = serverName
			if serverPort != "" {
				req.Params["SERVER_PORT"] = serverPort
			}
			if r.TLS != nil {
				req.Params["HTTPS"] = "on"
			}
			if ct := r.Header.Get("Content-Type"); ct != "" {
				req.Params["CONTENT_TYPE"] = ct
			}
			if cl := r.Header.Get("Content-Length"); cl != "" {
				req.Params["CONTENT_LENGTH"] = cl
			}
			if c := r.Header.Get("Cookie"); c != "" {
				req.Params["HTTP_COOKIE"] = c
			}
			return gofast.BasicSession(client, req)
		},
		clientFactory,
	)

	fcgiHandler.ServeHTTP(w, r)
	return true
}

func (p *PHPPlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}
func (p *PHPPlugin) OnExit() error                                       { return nil }

// fpmFactories caches one FastCGI client factory per FPM address. One
// connection is dialed per request (as nginx does with FPM by default):
// pooling idle FastCGI connections pins php-fpm workers, which deadlocks
// pools with few children. gofast's eager ClientPool was evaluated and
// rejected for exactly that reason.
var fpmFactories sync.Map // addr -> gofast.ClientFactory

func fpmClientFactory(addr string) gofast.ClientFactory {
	if cached, ok := fpmFactories.Load(addr); ok {
		return cached.(gofast.ClientFactory)
	}

	network := "tcp"
	if strings.HasPrefix(addr, "/") {
		network = "unix"
	}
	factory := gofast.SimpleClientFactory(gofast.SimpleConnFactory(network, addr))

	actual, _ := fpmFactories.LoadOrStore(addr, factory)
	return actual.(gofast.ClientFactory)
}

func phpScriptPath(root, cleanPath string) (string, error) {
	relPath := strings.TrimPrefix(cleanPath, "/")
	if relPath == "" {
		return root, nil
	}

	localPath, err := filepath.Localize(relPath)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(root, localPath)

	// Defense in depth: confirm the resolved path never escapes the document
	// root, even if Localize behaves unexpectedly on some platform.
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes document root")
	}
	return joined, nil
}
