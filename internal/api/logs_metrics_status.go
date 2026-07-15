package api

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/monitor"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

var startTime = time.Now()
var requestsTotal uint64

func LogsSnapshot() ([]byte, error) {
	logDir := config.GetLogDir()
	var logs []byte
	err := filepath.Walk(logDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			logs = append(logs, data...)
			logs = append(logs, '\n')
		}
		return nil
	})
	return logs, err
}

func getLogsHandler(w http.ResponseWriter, r *http.Request) {
	logs, err := LogsSnapshot()
	if err != nil {
		http.Error(w, "Unable to read log file", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(logs)
}

func MetricsSnapshot() map[string]any {
	cpuPercent, _ := cpu.Percent(0, false)
	vm, _ := mem.VirtualMemory()

	activePlugins := 0
	config.GlobalConfMu.RLock()
	if config.GlobalConf != nil {
		activePlugins = len(config.GlobalConf.EnabledPlugins)
	}
	config.GlobalConfMu.RUnlock()

	config.SiteConfigsMu.RLock()
	activeSites := len(config.SiteConfigs)
	config.SiteConfigsMu.RUnlock()

	requests := monitor.RequestCount()
	if requests == 0 {
		requests = atomic.LoadUint64(&requestsTotal)
	}

	return map[string]any{
		"requests_total": requests,
		"latency_avg_ms": 0,
		"cpu_usage":      cpuPercent,
		"ram_usage_mb":   vm.Used / 1024 / 1024,
		"active_sites":   activeSites,
		"active_plugins": activePlugins,
	}
}

func getMetricsHandler(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&requestsTotal, 1)
	metrics := MetricsSnapshot()
	jsonResponse(w, metrics)
}

func StatusSnapshot() map[string]any {
	plugins := []string{}
	config.GlobalConfMu.RLock()
	if config.GlobalConf != nil {
		plugins = append(plugins, config.GlobalConf.EnabledPlugins...)
	}
	config.GlobalConfMu.RUnlock()

	config.SiteConfigsMu.RLock()
	sites := len(config.SiteConfigs)
	config.SiteConfigsMu.RUnlock()

	return map[string]any{
		"uptime":   time.Since(startTime).String(),
		"sites":    sites,
		"plugins":  plugins,
		"apiAlive": true,
	}
}

func getStatusHandler(w http.ResponseWriter, r *http.Request) {
	status := StatusSnapshot()
	jsonResponse(w, status)
}

func LogWeightSnapshot() (int64, error) {
	logDir := config.GetLogDir()
	var totalSize int64 = 0
	err := filepath.Walk(logDir, func(_ string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	return totalSize, err
}

func getLogWeightHandler(w http.ResponseWriter, r *http.Request) {
	totalSize, err := LogWeightSnapshot()
	if err != nil {
		http.Error(w, "Error calculating log weight", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]any{
		"log_weight_bytes": totalSize,
	})
}

func PluginUsageSnapshot() map[string]int {
	usage := make(map[string]int)
	config.SiteConfigsMu.RLock()
	defer config.SiteConfigsMu.RUnlock()
	for _, site := range config.SiteConfigs {
		for pluginName := range site.PluginConfigs {
			usage[pluginName]++
		}
	}
	return usage
}

func getPluginUsageHandler(w http.ResponseWriter, r *http.Request) {
	usage := PluginUsageSnapshot()
	jsonResponse(w, usage)
}
