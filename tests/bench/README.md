# Benchmarks

Reproducible micro-benchmarks for the request hot paths (static serving, gzip,
logging, plugin dispatch, plugin proxying). They are the acceptance gate for
performance work: a change lands only if `benchstat` shows a stable
improvement in the relevant scenario with no regression elsewhere.

## Running

```bash
go test -run '^$' -bench . -benchmem -count=10 \
  ./internal/server ./internal/server/middleware ./internal/plugin ./plugins \
  | tee results.txt
benchstat baseline.txt results.txt
```

## Files

- `baseline.txt` - measured baseline before the performance work started
  (commit, environment and CPU recorded in the header).
- `after-perf-round1.txt` - same suite after the first optimization round.

## Reading the numbers honestly

- `B/op` and `allocs/op` are deterministic and comparable across sessions.
- Wall-clock (`sec/op`) on laptops drifts with thermals: two suites run at
  different times can show 20-40% phantom deltas in either direction. To
  validate a wall-clock delta, run an interleaved A/B on the same machine
  (alternate `git stash` / `git stash pop` with `-count=3` a few times and
  feed the concatenated outputs to `benchstat`).
- The proxy benchmarks (`NodeProxy_*`) include a local HTTP backend, so they
  measure the full plugin proxy path end to end.
