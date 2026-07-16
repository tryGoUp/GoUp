package safeguard

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/restart"
	"github.com/shirou/gopsutil/process"
)

// self is the current process handle, used to read the live resident set size.
var self *process.Process

var log *logger.Logger

// Start starts the SafeGuard routine if enabled.
func Start() {
	config.GlobalConfMu.RLock()
	globalConf := config.GlobalConf
	config.GlobalConfMu.RUnlock()

	if globalConf == nil {
		return
	}
	conf := globalConf.SafeGuard

	// Default: Enabled if not explicitly set to false
	if conf.Enable != nil && !*conf.Enable {
		return
	}

	// Default: 1024MB if not set
	limit := conf.MaxMemoryMB
	if limit <= 0 {
		limit = 1024
	}

	// Initialize logger
	var err error
	log, err = logger.NewSystemLogger("SafeGuard")
	if err != nil {
		fmt.Printf("[SafeGuard] Error initializing logger: %v\n", err)
		return
	}

	interval := 30 * time.Second
	if conf.CheckInterval != "" {
		if d, err := time.ParseDuration(conf.CheckInterval); err == nil {
			interval = d
		}
	}

	if p, err := process.NewProcess(int32(os.Getpid())); err == nil {
		self = p
	} else {
		log.Errorf("[SafeGuard] Unable to read process memory, disabling: %v", err)
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			checkMemory(limit)
		}
	}()

	log.Infof("SafeGuard Active: Limit=%dMB, Interval=%s", limit, interval)
}

func checkMemory(limitMB int) {
	if self == nil {
		return
	}
	// Use the live resident set size (actual physical memory in use), not
	// runtime.MemStats.Sys, which reports reserved address space and never
	// meaningfully shrinks, causing false restarts.
	memInfo, err := self.MemoryInfo()
	if err != nil {
		log.Errorf("[SafeGuard] Failed to read memory info: %v", err)
		return
	}
	usageMB := int(memInfo.RSS / 1024 / 1024)

	if usageMB > limitMB {
		log.Errorf("CRITICAL: Memory usage %dMB exceeded limit %dMB. FORCE RESTARTING...", usageMB, limitMB)

		// Auto-dump heap profile for debugging
		logDir := config.GetLogDir()
		if err := os.MkdirAll(logDir, 0755); err == nil {
			dumpFile := filepath.Join(logDir, fmt.Sprintf("heap-dump-%d.pprof", time.Now().Unix()))
			f, err := os.Create(dumpFile)
			if err == nil {
				pprof.WriteHeapProfile(f)
				f.Close()
				log.Infof("Heap profile saved to %s", dumpFile)
			} else {
				log.Errorf("Failed to create heap profile: %v", err)
			}
		}

		// Trigger forced restart
		restart.ForceRestart()
	}
}
