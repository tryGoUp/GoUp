# GoUp Native Authoritative DNS Server

GoUp includes a built-in, lightweight authoritative DNS server written in Go. It runs alongside the web server (or standalone) and shares the same high-performance architecture.

## Features

- **Authoritative Only**: Designed to serve your zones, not to act as a recursive resolver.
- **Record Types**: Supports `A`, `AAAA`, `CNAME`, `TXT`, `MX`, `NS`.
- **Integrated Logging**: detailed query logging to `logs/system/`.
- **Performance**: Runs on its own goroutine with low overhead.
- **Failover**: Optional upstream resolvers for non-authoritative zones (limited forwarding).

## Configuration

The DNS server is configured in the global configuration file (`~/.config/goup/conf.global.json`).

### Enabling DNS

```json
{
  "dns": {
    "enable": true,
    "port": 53,
    "upstream_resolvers": ["1.1.1.1", "8.8.8.8"],
    "zones": {
      "example.com": [
        { "type": "A", "name": "@", "value": "192.168.1.10", "ttl": 300 },
        { "type": "CNAME", "name": "www", "value": "@", "ttl": 300 },
        { "type": "TXT", "name": "_test", "value": "hello world", "ttl": 3600 }
      ]
    }
  }
}
```

### Fields

- **enable**: Set to `true` to start the DNS server.
- **port**: UDP/TCP port to listen on (default: 53). *Note: Ports < 1024 require root/sudo.*
- **upstream_resolvers**: List of DNS servers to forward queries to if no local zone matches (optional).
- **zones**: Map of zone names to their records.

### Record Structure

- **type**: Record type (`A`, `AAAA`, `CNAME`, `TXT`, `MX`, `NS`).
- **name**: Subdomain name. Use `@` for the zone apex (e.g. `example.com`), or just the name (e.g. `www`).
- **value**: The IP address, target domain, or text content.
- **ttl**: Time-To-Live in seconds.
- **prio**: Priority (only for `MX` records).

## Running the Server

You can run the DNS server in different modes using the CLI:

1.  **Full Stack (Web + DNS)** (Default)
    ```bash
    goup start
    ```
2.  **DNS Only**
    ```bash
    goup start-dns
    ```
3.  **Web Only**
    ```bash
    goup start-web
    ```

## Specialized Builds

For deployment in constrained environments, you can compile GoUp with only the components you need to save binary size.

- **Build DNS-Only Binary**:
  ```bash
  go build -tags dns_only -o goup-dns cmd/goup/main.go
  ```

- **Build Web-Only Binary**:
  ```bash
  go build -tags web_only -o goup-web cmd/goup/main.go
  ```
