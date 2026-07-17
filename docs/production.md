# Running GoUp in production

This guide covers running GoUp as a long-lived service: as a systemd unit, in a
container, binding privileged ports without root, reloading configuration, and
log retention.

## Directory layout

GoUp resolves its paths from the XDG environment variables:

- Configuration: `$XDG_CONFIG_HOME/goup` (per-site `*.json` plus `conf.global.json`)
- Logs and state: `$XDG_DATA_HOME/goup/logs`

For a system service the recommended layout is:

- `XDG_CONFIG_HOME=/etc` so configuration lives in `/etc/goup`
- `XDG_DATA_HOME=/var/lib` so logs live in `/var/lib/goup/logs`

## systemd

A ready-to-use unit is provided at [`init/goup.service`](../init/goup.service).

```bash
# Create a dedicated service user and directories
sudo useradd --system --home /var/lib/goup --shell /usr/sbin/nologin goup
sudo mkdir -p /etc/goup /var/lib/goup
sudo chown -R goup:goup /etc/goup /var/lib/goup

# Install the binary and the unit
sudo install -m 0755 goup /usr/local/bin/goup
sudo install -m 0644 init/goup.service /etc/systemd/system/goup.service

sudo systemctl daemon-reload
sudo systemctl enable --now goup
```

The unit grants `CAP_NET_BIND_SERVICE`, so GoUp can bind ports 80 and 443
without running as root.

Reload configuration and certificates without downtime:

```bash
sudo systemctl reload goup   # sends SIGHUP
```

## Binding ports 80/443 as non-root

If you do not use the systemd unit, grant the capability directly to the binary:

```bash
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/goup
```

## Docker

```bash
docker build -t goup .
docker run -d --name goup \
  -p 80:80 -p 443:443 \
  -v /etc/goup:/etc/goup \
  -v /var/lib/goup:/var/lib/goup \
  goup
```

The image is based on distroless `nonroot`; mount your configuration into
`/etc/goup`.

## HTTP to HTTPS redirect

Create two site configs for the same domain: one on port 80 with
`"force_https": true`, and one on port 443 with TLS enabled. Requests to the
plain-HTTP listener are 301-redirected to HTTPS. Enable `"hsts": true` on the
HTTPS site to send `Strict-Transport-Security`.

## Log retention

Set `log_retention_days` in `conf.global.json` to automatically delete log files
(and heap dumps) older than N days. `0` (the default) keeps logs forever.

## Health checks

Every site exposes an unauthenticated `/up` endpoint that returns `200 ok` when
ready and `503` while the server is draining during shutdown. Point your load
balancer or orchestrator liveness/readiness probe at it.
