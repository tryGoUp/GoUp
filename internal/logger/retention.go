package logger

import (
	"os"
	"path/filepath"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
)

// StartRetention launches a background sweeper that deletes log files older than
// retentionDays. It is a no-op when retentionDays <= 0 (keep logs forever).
func StartRetention(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	go func() {
		// Run once at startup, then daily.
		purgeOldLogs(retentionDays)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			purgeOldLogs(retentionDays)
		}
	}()
}

func purgeOldLogs(retentionDays int) {
	root := config.GetLogDir()
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Only prune log and heap-dump artifacts, and only when stale.
		ext := filepath.Ext(path)
		if ext != ".log" && ext != ".pprof" {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
		}
		return nil
	})
}
