<div align="center">
    <img src="https://raw.githubusercontent.com/tryGoUp/brand/refs/heads/main/Logo-Text/Light/logo-text-light.png#gh-light-mode-only" height="100">
    <img src="https://raw.githubusercontent.com/tryGoUp/brand/refs/heads/main/Logo-Text/Dark/logo-text-dark.png#gh-dark-mode-only" height="100">
    <hr />
    <p>A Minimal Configurable Web Server in Go</p>
</div>

GoUP! is a minimal, tweakable web server written in Go. You can use it to serve static files, set up reverse proxies, and configure SSL for multiple domains, all through simple JSON configuration files. GoUp spawns a dedicated server for each port, websites with the same port are treated as virtual hosts and run on the same server.

## Features

- Serve static files from a specified root directory
- Reverse proxy to a single backend or load-balance across several (`proxy_upstreams`) with health checks and failover
- SSL/TLS with custom certificates or automatic Let's Encrypt certificates (`ssl.acme`)
- HTTP-to-HTTPS redirect, HSTS, and configurable security headers
- Edge hardening: per-IP rate limiting, IP allow/deny lists, request body limits, CORS
- Custom headers and `Cache-Control` for HTTP responses
- Support for multiple domains and virtual hosting
- Native Authoritative DNS Server (A, AAAA, CNAME, TXT, MX, NS)
- Logging to both console and files - JSON formatted (structured logs), with date rotation and retention
- Zero-downtime config reload (`SIGHUP`), graceful shutdown, and a memory watchdog (SafeGuard)
- Optional TUI interface for real-time monitoring
- HTTP/2 and HTTP/3 support (not configurable, HTTP/1.1 is used for unencrypted connections, HTTP/2 and HTTP/3 for encrypted connections)

## Documentation

- [DNS Server Guide](docs/dns.md) explanation of the DNS module configuration and usage.
- [Kamal Proxy Guide](docs/kamal-proxy.md) for blue/green deployments with kamal-proxy.

## API & Dashboard

GoUp includes a built-in REST API and a Web Dashboard for management.
**By default, these are disabled for security reasons.**

To enable them, edit your global configuration file (`~/.config/goup/conf.global.json`) and configure the `account` section to secure them.

### Authentication

Security is mandatory when enabling the API/Dashboard, and it is enforced:
GoUp refuses to start the API without an `api_token`, and refuses to start the
Dashboard without a `username` and `password_hash`. GoUp uses:
- **Basic Auth** for the Dashboard.
- **Token Auth** for the API.

Configuration example:

```json
{
  "account": {
    "username": "admin",
    "password_hash": "$2a$12$R9h/cIPz0gi.URNNXMnmueKz3hJ...", // BCrypt hash
    "api_token": "your-secret-token-here"
  },
  "enable_api": true,
  "api_port": 6007,
  "dashboard_port": 6008
}
```

> **Note:** You can generate a BCrypt hash using online tools or `htpasswd -Bnm user password`.

## Compression

GoUp handles compression automatically with a dual-layer strategy:

1.  **Pre-compressed Files**: Checks for `.br` or `.gz` sidecar files (e.g., `style.css.gz`) and serves them directly if available.
2.  **On-The-Fly**: If no pre-compressed file is found, it uses Gzip compression on the fly for compressible content types (HTML, CSS, JSON, etc.).

## SafeGuard (Auto-Restart)

GoUp includes a built-in **SafeGuard** system that monitors memory usage and automatically restarts the process if it exceeds safety limits, ensuring long-term stability.

- **Enabled by Default**: Checks resident memory (RSS) every 30 seconds.
- **Auto-Dump**: Saves a Pprof heap dump before restarting for easier debugging.
- **Seamless Restart**: Uses `syscall.Exec` to replace the process immediately.

Configuration (in `~/.config/goup/conf.global.json`):

```json
{
  "safeguard": {
    "enable": true,
    "max_memory_mb": 1024,
    "check_interval": "30s"
  }
}
```

`check_interval` is a Go duration string (e.g. `"30s"`, `"1m"`).

## Installation

Go is required to build the software, ensure you have it installed on your system.

1. **Clone the repository:**

   ```bash
   git clone https://github.com/mirkobrombin/goup.git
   cd goup
   ```

2. **Build the software:**

   Standard build (includes both Web and DNS modules):
   ```bash
   go build -o ~/.local/bin/goup cmd/goup/main.go
   ```

   **Specialized Builds**:
   You can build optimized binaries using build tags to exclude unused modules:

   *Web Server Only*:
   ```bash
   go build -tags web_only -o goup-web cmd/goup/main.go
   ```

   *DNS Server Only*:
   ```bash
   go build -tags dns_only -o goup-dns cmd/goup/main.go
   ```


## Usage

### Generating a Configuration

The `generate` command can be used to create a new site configuration:

```bash
goup generate [domain]
```

Example:

```bash
goup generate example.com
```

it will prompt you to enter the following details:

- Port number
- Root directory
- Proxy settings
- Custom headers
- SSL configuration
- Request timeout

The configuration file will be saved in `~/.config/goup/` as `[domain].json`, you
can edit or create new configurations manually, just restart the server to apply changes.

Read more about the configuration structure in the [Configuration](#configuration) section.

### Starting the Server

Start the server with:

```bash
goup start
```

This command loads all configurations from `~/.config/goup/` and starts serving.

**Starting with TUI Mode:**

Enable the Text-Based User Interface to monitor logs:

```bash
goup start --tui
```

### Additional Commands

- **List Configured Sites:**

  ```bash
  goup list
  ```

- **Validate Configurations:**

  ```bash
  goup validate
  ```

- **Stop the Server:**

  ```bash
  goup stop
  ```

- **Restart the Server:**

  ```bash
  goup restart
  # Or, if running under systemd, reload without downtime:
  #   systemctl reload goup   (sends SIGHUP)
  ```

- **Print the Version:**

  ```bash
  goup version
  ```

- **Generate Password Hash:**

  ```bash
  goup gen-pass
  # Or providing the password as argument
  goup gen-pass mysecretpassword
  ```

## Configuration

### Site Configuration Structure

Each site configuration is represented by a JSON file and meets the following structure:

```json
{
  "domain": "example.com",
  "port": 8080,
  "root_directory": "/path/to/root",
  "custom_headers": {
      "X-Domain-Name": "example.com"
  },
  "proxy_pass": "http://localhost:3000",
  "ssl": {
    "enabled": true,
    "certificate": "/path/to/cert.crt",
    "key": "/path/to/key.key"
  },
  "request_timeout": 60
}
```

**Fields:**

- **domain**: The domain name for which the server will respond
- **port**: The port number to listen on
- **root_directory**: Path to the directory containing static files. Leave empty if using `proxy_pass`
- **custom_headers**: Key-value pairs of custom headers to include in responses
- **proxy_pass**: URL to the backend service for reverse proxying. Leave empty if serving static files
- **proxy_upstreams**: List of backend URLs to load-balance across (round-robin with failover). Takes precedence over `proxy_pass`
- **ssl**:
  - **enabled**: Set to `true` to enable SSL/TLS
  - **certificate**: Path to the SSL certificate file
  - **key**: Path to the SSL key file
  - **acme**: Set to `true` to obtain and renew a Let's Encrypt certificate automatically (ignores certificate/key). Requires the domain to resolve to this host and port 443 to be reachable
  - **email**: ACME account email (recommended)
  - **cache_dir**: Where issued certificates are cached
- **request_timeout**: Read timeout for client requests in seconds (default 60; `-1` disables it)

**Additional site fields (all optional):**

| Field | Type | Description |
|---|---|---|
| `read_header_timeout` | int (s) | Header-read timeout (default 10) |
| `idle_timeout` | int (s) | Keep-alive idle timeout (default 120) |
| `max_header_bytes` | int | Maximum request header size |
| `max_body_bytes` | int | Maximum request body size (0 = unlimited) |
| `proxy_flush_interval` | duration | Reverse-proxy flush interval (e.g. `"100ms"`) |
| `buffer_size_kb` | int | Reverse-proxy copy buffer size in KB |
| `max_concurrent_connections` | int | Cap on in-flight requests (503 when exceeded) |
| `enable_logging` | bool | Per-site access logging (default true) |
| `file_server_mode` | bool | Plain directory listing, no branded pages |
| `force_https` | bool | Redirect plain HTTP to HTTPS (put on the :80 site) |
| `hsts` | bool | Send `Strict-Transport-Security` when served over TLS |
| `hsts_max_age` | int (s) | HSTS max-age (default 31536000) |
| `security_headers` | bool | Add `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy` |
| `cache_control` | string | `Cache-Control` value for static responses |
| `allow_ips` / `deny_ips` | []CIDR | IP allow/deny lists |
| `rate_limit_rps` | float | Per-IP requests/second (0 = disabled) |
| `rate_limit_burst` | int | Per-IP burst size |
| `cors` | object | CORS: `allowed_origins`, `allowed_methods`, `allowed_headers`, `allow_credentials`, `max_age` |
| `plugin_configs` | object | Per-plugin configuration (see Plugins) |

Global settings (`conf.global.json`) also support `api_bind` / `dashboard_bind`
(bind addresses, empty = all interfaces) and `log_retention_days` (auto-purge
logs older than N days, 0 = keep forever).

Run `goup validate` to check every config file: it reports JSON typos (unknown
fields), missing certificate/root paths, invalid ports, and cross-site conflicts
(duplicate domains, or a port mixing SSL and non-SSL sites).

See [Running in production](docs/production.md) for systemd, Docker, non-root
port binding, and zero-downtime reloads.

## Logging

Logs are written to both the console and log files, those are stored in:

```
~/.local/share/goup/logs/[identifier]/[year]/[month]/[day].log
```

- **identifier**: The domain name or `port_[port_number]` for virtual hosts
- Logs are formatted in JSON for easy parsing

## TUI Interface

Enable the TUI with the `--tui` flag when starting the server:

```bash
goup start --tui
```

## Plugins

GoUP! has a lightweight plugin system that allows you to extend its 
functionality. Plugins implement a set of hooks for initialization, request 
handling, and cleanup:

- **OnInit()**: Called once when GoUP! starts (useful for global setup).
- **OnInitForSite(conf config.SiteConfig, logger *log.Logger)**: Called for 
each site configuration (site-specific setup). Within this, you usually call 
a helper like `p.SetupLoggers(conf, p.Name(), logger)` if you’re using the 
built-in \`BasePlugin\` approach for loggers.
- **BeforeRequest(r *http.Request)**: Invoked before every request, letting 
you examine or modify the incoming request.
- **HandleRequest(w http.ResponseWriter, r *http.Request) bool**: If your 
plugin wants to fully handle the request (e.g., returning a response on its 
own), do it here. Return `true` if the request was fully handled (so GoUP! 
won’t process it further).
- **AfterRequest(w http.ResponseWriter, r *http.Request)**: Called after each 
request has been served or intercepted by this plugin.
- **OnExit()**: Called once when GoUP! is shutting down (for cleanup).

### Enabling Plugins

To enable plugins, add their configuration in the `plugin_configs` section of 
the site’s JSON configuration file. For example:

```json
{
  "domain": "example.com",
  "port": 8080,
  "root_directory": "/path/to/root",
  "custom_headers": {
    "X-Custom-Header": "Hello, World!"
  },
  "plugin_configs": {
    "PHPPlugin": {
      "enable": true,
      "fpm_addr": "/run/php/php8.2-fpm.sock"
    },
    "AuthPlugin": {
      "protected_paths": ["/protected.html"],
      "credentials": {
        "admin": "password123",
        "user": "userpass"
      },
      "session_expiration": 3600
    },
    "NodeJSPlugin": {
      "enable": true,
      "port": "3000",
      "root_dir": "/path/to/node/app",
      "entry": "server.js",
      "install_deps": true,
      "node_path": "/usr/bin/node",
      "package_manager": "pnpm",
      "proxy_paths": ["/api/", "/backend/"]
    }
  }
}
```

### Example: NodeJSPlugin

Below is an **excerpt** from the built-in **NodeJSPlugin**. Notice how it embeds 
a `BasePlugin` for convenient log handling:

```go
type NodeJSPlugin struct {
    plugin.BasePlugin  // provides DomainLogger + PluginLogger
    mu       sync.Mutex
    process  *os.Process

    siteConfigs map[string]NodeJSPluginConfig
}

func (p *NodeJSPlugin) OnInitForSite(conf config.SiteConfig, domainLogger *log.Logger) error {
    // Initialize domain + plugin loggers (for console vs plugin-specific logs)
    if err := p.SetupLoggers(conf, p.Name(), domainLogger); err != nil {
        return err
    }
    // Parse plugin-specific JSON config, etc.
    ...
}
```

Inside the code, we have **two** loggers:
- **`p.DomainLogger`** for messages shown in the console + domain log file 
(e.g., "Delegating path=... to Node.js").
- **`p.PluginLogger`** for plugin-specific logs only (in the dedicated `NodeJSPlugin.log`).

### Pre-Installed Plugins

- **Custom Header Plugin**: Adds custom headers to HTTP responses.
- **PHP Plugin**: Handles `.php` requests using PHP-FPM.
- **Auth Plugin**: Protects routes with basic authentication.
- **NodeJS Plugin**: Handles Node.js applications with Node.

Each plugin can have its own JSON configuration under `plugin_configs`, which it 
reads in `OnInitForSite`.

### Developing Plugins

You can create your own plugins by implementing the following interface:

```go
type Plugin interface {
    Name() string
    OnInit() error
    OnInitForSite(conf config.SiteConfig, logger *log.Logger) error
    BeforeRequest(r *http.Request)
    HandleRequest(w http.ResponseWriter, r *http.Request) bool
    AfterRequest(w http.ResponseWriter, r *http.Request)
    OnExit() error
}
```

A minimal example plugin:

```go
package myplugin

import (
    "net/http"
    "github.com/mirkobrombin/goup/internal/config"
    "github.com/mirkobrombin/goup/internal/plugin"
    log "github.com/sirupsen/logrus"
)

type MyPlugin struct {
    plugin.BasePlugin // optional embedding if you want domain + plugin logs
}

func (p *MyPlugin) Name() string {
    return "MyPlugin"
}
func (p *MyPlugin) OnInit() error {
    return nil
}
func (p *MyPlugin) OnInitForSite(conf config.SiteConfig, logger *log.Logger) error {
    // If you want domain + plugin logs:
    p.SetupLoggers(conf, p.Name(), logger)
    return nil
}
func (p *MyPlugin) BeforeRequest(r *http.Request) {}
func (p *MyPlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
    return false
}
func (p *MyPlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}
func (p *MyPlugin) OnExit() error {
    return nil
}
```

Then register your plugin in the `main.go` file:

```go
pluginManager.Register(&myplugin.MyPlugin{})
```

## Contributing

I really appreciate any contributions you would like to make, whether it's a 
simple typo fix or a new feature. Feel free to open an issue or submit a pull request.

## Pro Tips

You can use the `public/` directory in the repository as the root directory for
your test sites. It contains a simple `index.html` file with a JS script that
gets the website's title from the `X-Domain-Name` header (if set).

## License

GoUP! is released under the [MIT License](LICENSE).

---

**Note:** Review the [production guide](docs/production.md) before deploying:
run GoUp as a non-root service, secure the API/Dashboard with credentials, and
enable TLS, HSTS, and rate limiting for internet-facing sites.
