# System-level load test: pre-optimization vs post-optimization

Date: 2026-07-15. Machine: i7-13700H, Linux 6.17.9+deb14-amd64, Go go1.26.0.
Old binary: commit 2cf14ff (before the performance work). New binary: commit 5885aa8.
Tool: bombardier, 125 connections, 10s per scenario, Accept-Encoding: gzip, two alternated rounds.
Both instances ran simultaneously on separate ports against the same fixtures.

| Scenario | old rps | new rps | delta | old lat | new lat |
|---|---|---|---|---|---|
| static 128B | 41-56k | 38-44k | ~neutral (noise) | 2.2-3.1ms | 2.8-3.3ms |
| static 8KB html | 27-31k | 29-35k | +7..13% | 4.1-4.6ms | 3.6-4.3ms |
| static 1MB | 5.4-5.7k | 14.0-14.7k | **+156% (2.6x)** | 21.9-23.2ms | 8.5-8.9ms |
| proxy 16KB | 13.9-14.3k | 20.0-20.1k | **+40..44%** | 8.7-9.0ms | 6.2-6.3ms |

Zero non-200 responses in every run.

The 1MB gain comes from the gzip wrapper now preserving io.ReaderFrom
(sendfile) for non-compressible content; the proxy gain from the tuned
shared transport (idle connection pool instead of the 2-connection default).

HTTPS verified end to end on the new binary: HTTP/2 with
`alt-svc: h3=":8443"` advertised, and a real QUIC request served as
HTTP/3.0 by a quic-go client.
