# Kamal Proxy

GoUp can run as an HTTP target behind [kamal-proxy](https://github.com/basecamp/kamal-proxy).

Kamal Proxy handles the blue/green switch. GoUp only needs to expose a health endpoint and stop cleanly when the old process is removed.

## Target contract

GoUp exposes:

```text
GET /up
```

The endpoint returns:

- `200 OK` while GoUp is ready to accept traffic.
- `503 Service Unavailable` after GoUp starts shutting down and before the listener closes.

The endpoint is handled before static files, reverse proxies, plugins, and virtual host routing. This means kamal-proxy can use the default health check path even when the target host header does not match a configured site.

On shutdown, GoUp marks itself not ready, calls `http.Server.Shutdown` for every web server, and waits up to 10 seconds before exiting. Health checks may receive `503` during that short window, or fail to connect after the listener closes.

## GoUp configuration

Run GoUp without site TLS when kamal-proxy terminates TLS:

```json
{
  "domain": "example.com",
  "port": 3000,
  "root_directory": "/var/www/example.com",
  "ssl": {
    "enabled": false
  },
  "request_timeout": 60
}
```

Start GoUp:

```bash
goup start-web --config /etc/goup/example.com.json
```

## Proxy setup

Start kamal-proxy on the public ports:

```bash
kamal-proxy run --http-port 80 --https-port 443
```

Deploy the first GoUp target:

```bash
kamal-proxy deploy goup \
  --target 127.0.0.1:3000 \
  --host example.com \
  --tls
```

Kamal Proxy checks `GET /up` once per second by default. When the new target returns `200`, new requests move to it. The command returns only after the previous target has drained.

## Blue/green flow

1. Start the new GoUp process on a free port, for example `3001`.
2. Deploy it to kamal-proxy:

   ```bash
   kamal-proxy deploy goup \
     --target 127.0.0.1:3001 \
     --host example.com \
     --tls
   ```

3. Stop the old GoUp process after the deploy command succeeds:

   ```bash
   kill -TERM <old-goup-pid>
   ```

If the new target does not become healthy before the deploy timeout, kamal-proxy exits with a non-zero status and keeps routing to the old target.

## Notes

- Keep `/up` reserved for health checks.
- Use `--health-check-path` only if a future deployment needs a custom path.
- Use the kamal-proxy CLI for deployments. GoUp does not call the proxy's internal RPC socket directly.
