package server

import (
	"context"
	"os"
	"time"

	"github.com/mirkobrombin/go-warp/v1/cache"
)

// statCache caches os.Stat results (including "not found", the negative case)
// for a short TTL, mirroring nginx's open_file_cache. The static hot path
// stats up to four paths per request (file, index.html, .br, .gz sidecars);
// under load the same paths repeat, so a 1-second cache removes almost all of
// those syscalls while keeping staleness bounded.
//
// The backing store is go-warp's in-memory cache: TTL expiry, LRU eviction
// and the max-entries bound are handled there instead of by hand-rolled
// sync.Map swapping.
const statCacheTTL = time.Second

// statCacheMaxEntries bounds memory usage: past this, the oldest entries are
// evicted (e.g. during a scan of random 404 URLs).
const statCacheMaxEntries = 65536

type statEntry struct {
	info os.FileInfo
	err  error
}

var statCache = cache.NewInMemory[statEntry](
	cache.WithMaxEntries[statEntry](statCacheMaxEntries),
	cache.WithSweepInterval[statEntry](time.Minute),
)

// resetStatCache drops every cached entry. Tests use it when they mutate the
// filesystem and need the change visible before the TTL expires.
func resetStatCache() {
	statCache.Close()
	statCache = cache.NewInMemory[statEntry](
		cache.WithMaxEntries[statEntry](statCacheMaxEntries),
		cache.WithSweepInterval[statEntry](time.Minute),
	)
}

// cachedStat is a TTL-cached os.Stat.
func cachedStat(path string) (os.FileInfo, error) {
	ctx := context.Background()
	if e, ok, err := statCache.Get(ctx, path); err == nil && ok {
		return e.info, e.err
	}

	info, err := os.Stat(path)
	_ = statCache.Set(ctx, path, statEntry{info: info, err: err}, statCacheTTL)
	return info, err
}
