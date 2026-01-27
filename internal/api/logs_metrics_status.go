package api

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

var startTime = time.Now()
var requestsTotal uint64

func getLogsHandler(w http.ResponseWriter, r *http.Request) {
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
	if err != nil {
		http.Error(w, "Unable to read log file", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(logs)
}

func getMetricsHandler(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&requestsTotal, 1)
	cpuPercent, _ := cpu.Percent(0, false)
	vm, _ := mem.VirtualMemory()
	metrics := map[string]any{
		"requests_total": atomic.LoadUint64(&requestsTotal),
		"latency_avg_ms": 0,
		"cpu_usage":      cpuPercent,
		"ram_usage_mb":   vm.Used / 1024 / 1024,
		"active_sites":   len(config.SiteConfigs),
		"active_plugins": len(config.GlobalConf.EnabledPlugins),
	}
	jsonResponse(w, metrics)
}

func getStatusHandler(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{
		"uptime":   time.Since(startTime).String(),
		"sites":    len(config.SiteConfigs),
		"plugins":  config.GlobalConf.EnabledPlugins,
		"apiAlive": true,
	}
	jsonResponse(w, status)
}

func getLogWeightHandler(w http.ResponseWriter, r *http.Request) {
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
	if err != nil {
		http.Error(w, "Error calculating log weight", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]any{
		"log_weight_bytes": totalSize,
	})
}

func getPluginUsageHandler(w http.ResponseWriter, r *http.Request) {
	usage := make(map[string]int)
	for _, site := range config.SiteConfigs {
		for pluginName := range site.PluginConfigs {
			usage[pluginName]++
		}
	}
	jsonResponse(w, usage)
}
