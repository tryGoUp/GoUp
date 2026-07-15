package server

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// statCache caches os.Stat results (including "not found", the negative case)
// for a short TTL, mirroring nginx's open_file_cache. The static hot path
// stats up to four paths per request (file, index.html, .br, .gz sidecars);
// under load the same paths repeat, so a 1-second cache removes almost all of
// those syscalls while keeping staleness bounded.
const statCacheTTL = time.Second

// statCacheMaxEntries bounds memory usage: when the cache grows past this
// (e.g. a scan of random 404 URLs), it is swapped for a fresh one.
const statCacheMaxEntries = 65536

type statEntry struct {
	info    os.FileInfo
	err     error
	expires int64
}

var (
	statCacheMap  atomic.Pointer[sync.Map]
	statCacheSize atomic.Int64
)

func init() {
	statCacheMap.Store(&sync.Map{})
}

// resetStatCache drops every cached entry. Tests use it when they mutate the
// filesystem and need the change visible before the TTL expires.
func resetStatCache() {
	statCacheMap.Store(&sync.Map{})
	statCacheSize.Store(0)
}

// cachedStat is a TTL-cached os.Stat.
func cachedStat(path string) (os.FileInfo, error) {
	now := time.Now().UnixNano()
	m := statCacheMap.Load()

	if v, ok := m.Load(path); ok {
		e := v.(*statEntry)
		if now < e.expires {
			return e.info, e.err
		}
	}

	info, err := os.Stat(path)
	e := &statEntry{info: info, err: err, expires: now + int64(statCacheTTL)}

	if _, loaded := m.Swap(path, e); !loaded {
		if statCacheSize.Add(1) > statCacheMaxEntries {
			statCacheMap.Store(&sync.Map{})
			statCacheSize.Store(0)
		}
	}
	return info, err
}
