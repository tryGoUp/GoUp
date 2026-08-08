# Migration: go-cli-builder v3, go-foundation v2, go-warp

Branch: `migrate/foundation-v2-cli-builder-v3-warp` (commit `5b1a157`).

## What changed

### go-cli-builder v3 — `internal/cli/cli.go`

cobra was replaced by declarative struct-tag commands. Every command,
flag, short name and exit code is preserved:

- `generate [domain]`, `gen-pass [password]`, `start`, `start-web`,
  `start-dns`, `validate`, `list`, `plugins`, `stop`, `restart`, `version`
- global flags `--config/-c` and `--global-config` (parsed on the root,
  exposed to subcommands the same way cobra PersistentFlags did)
- `--tui/-t` and `--bench/-b` on `start`
- `--version` flag on the root plus the `version` subcommand
- `Before()` on the root replaces cobra's `PersistentPreRun` (global config
  load before any subcommand)

### go-foundation v2 — `internal/logger/logger.go`

zerolog was removed from `go.mod`. The logger now runs on
`go-foundation/v2/core/logger` with two custom sinks:

- `jsonWriterSink`: one JSON object per line on the date-partitioned
  rotating file writer (unchanged `YYYY/MM/DD*.log` layout)
- `consoleSink`: colored human-readable lines on stdout (termenv kept)

The public API (`NewLogger`, `NewPluginLogger`, `NewSystemLogger`,
`Info/Infof/Error/...`, `WithFields`, `SetOutput`, `Writer`) is unchanged,
so the ~20 call sites in `internal/` and `plugins/` needed no edits.

`plugins/contracts.go` adds compile-time conformance of all 7 plugins to
`plugin.Plugin` via `contracts.Assert`, so a broken plugin surface fails the
build instead of registration.

### go-warp v1.2.2 — `internal/server/stat_cache.go`

The hand-rolled `sync.Map` + atomic swap stat cache was replaced by
`go-warp/v1/cache.NewInMemory` with `WithMaxEntries(65536)` and a 1s TTL.
TTL expiry, LRU eviction and the max-entries bound are now handled by the
framework. `resetStatCache` (used by tests) recreates the cache.

## v2.3.2: configuration and vhost dispatch (second pass)

After `go-foundation` v2.3.2 added the missing capabilities, the remaining
gaps closed:

### `internal/config` — configuration collections

- `LoadAllConfigs` discovers site files through
  `configuration/source/dir` (glob `*.json`, `conf.global.json` excluded by
  the provider) and decodes each with the existing strict `LoadConfig`, so
  unknown fields keep failing as before.
- `ValidateAll` now expresses its two failure modes as Foundation collection
  rules: `SiteConfig.Validate` is the per-file `ItemValidator` (file name is
  the entry name) and duplicate-domain / SSL-port-conflict checks are
  `CrossValidator`s. `ValidateCollection` aggregates all problems in one
  pass. CLI output shape unchanged.

### `internal/server` — virtual host dispatch and SNI

- `startVirtualHostServer` dropped the hand-rolled radix tree for
  `httpx.VHostMux` (normalized host matching, configurable fallback). The
  first registered handler stays the fallback for unknown hosts, preserving
  previous behavior. `github.com/armon/go-radix` is gone from `go.mod`.
- `setupTLS` loads static certificates into `httpx.CertResolver`, which
  selects per-SNI; ACME hosts still fall through to the autocert manager.
  Host normalization is shared between mux and resolver.

### Still not migrated

`app` / `app/web` (action-based HTTP): GoUp *is* the server (proxy, plugins,
QUIC, TUI); the action model is for building applications, not servers.
DNS indexes stay as-is: pre-compiled at startup, no TTL hot path.

## Verification

- `go build ./...` clean
- `go vet ./...` clean
- `go test ./...`: 9 packages ok
- CLI smoke-tested: `--help`, `--version`, `version`, `validate`, `list`,
  `plugins`, `gen-pass`
