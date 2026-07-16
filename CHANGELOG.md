# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Automatic TLS via ACME/Let's Encrypt (TLS-ALPN-01) with on-disk certificate
  caching and renewal (`ssl.acme`, `ssl.email`, `ssl.cache_dir`).
- Per-IP rate limiting (`rate_limit_rps`, `rate_limit_burst`).
- IP allow/deny lists (`allow_ips`, `deny_ips`).
- Request body size limit (`max_body_bytes`).
- CORS handling (`cors`) and configurable security headers (`security_headers`,
  `hsts`, `hsts_max_age`).
- HTTP-to-HTTPS redirect (`force_https`) and `Cache-Control` for static assets
  (`cache_control`).
- `goup version` command and `--version` flag, with version metadata injected at
  release time.
- Configuration hot reload on `SIGHUP` (and `systemctl reload`).
- Log retention purge (`log_retention_days`).
- systemd unit, Dockerfile, and a production deployment guide.
- Bind-address options for the API and Dashboard (`api_bind`, `dashboard_bind`).

### Changed
- `goup validate` now performs real semantic validation: rejects unknown JSON
  fields, checks certificate/root paths, port ranges, and cross-site conflicts.
- CI now runs gofmt, vet, staticcheck, govulncheck, and race-enabled tests;
  release builds embed version metadata and publish SHA-256 checksums.

### Fixed
- Authentication for the API and Dashboard now fails closed when credentials are
  unset, and refuses to start without them.
- Virtual-host servers select the correct TLS certificate by SNI instead of
  silently serving plaintext.
- Auth-plugin sessions are bound to a random cookie token instead of the
  spoofable client IP.
- Plugin child processes (Node.js, Python, Docker) are terminated on shutdown and
  restart instead of being orphaned.
- On-the-fly gzip no longer breaks HTTP 304 revalidation.
- API and Dashboard servers now set read/idle timeouts (slowloris hardening).
- Fixed a goroutine/buffer leak when logging spawned-process output.
- DNS zone matching respects label boundaries; the forwarder enforces a
  recursion ACL and truncates oversized UDP responses.
- Path traversal hardening across the API, config loading, and static serving.
