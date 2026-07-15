package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachedStat(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")

	t.Run("negative result is cached until TTL", func(t *testing.T) {
		resetStatCache()

		if _, err := cachedStat(target); !os.IsNotExist(err) {
			t.Fatalf("expected not-exist, got %v", err)
		}

		// The file appears, but within the TTL the cache still says no.
		os.WriteFile(target, []byte("x"), 0644)
		if _, err := cachedStat(target); !os.IsNotExist(err) {
			t.Fatalf("expected cached not-exist within TTL, got %v", err)
		}

		// After the TTL the new state is visible.
		time.Sleep(statCacheTTL + 50*time.Millisecond)
		info, err := cachedStat(target)
		if err != nil || info.Size() != 1 {
			t.Fatalf("expected file visible after TTL, got info=%v err=%v", info, err)
		}
	})

	t.Run("positive result served from cache", func(t *testing.T) {
		resetStatCache()

		first, err := cachedStat(target)
		if err != nil {
			t.Fatal(err)
		}
		os.Remove(target)
		second, err := cachedStat(target)
		if err != nil {
			t.Fatalf("expected cached hit within TTL, got %v", err)
		}
		if first.ModTime() != second.ModTime() {
			t.Fatal("expected identical cached FileInfo")
		}
	})

	t.Run("reset makes changes visible immediately", func(t *testing.T) {
		if _, err := cachedStat(target); err != nil {
			t.Fatalf("expected cached entry, got %v", err)
		}
		resetStatCache()
		if _, err := cachedStat(target); !os.IsNotExist(err) {
			t.Fatalf("expected not-exist after reset, got %v", err)
		}
	})
}
