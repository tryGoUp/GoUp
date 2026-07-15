package dashboard

import (
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mirkobrombin/goup/internal/api"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/monitor"
)

func streamDashboardHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if err := datastarPatchElements(w, dashboardFragments()); err != nil {
			return
		}
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func datastarPatchElements(w http.ResponseWriter, fragments string) error {
	if _, err := fmt.Fprintln(w, "event: datastar-patch-elements"); err != nil {
		return err
	}
	for _, line := range strings.Split(fragments, "\n") {
		if _, err := fmt.Fprintf(w, "data: elements %s\n", line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func dashboardFragments() string {
	sites := api.SitesSnapshot()
	metrics := api.MetricsSnapshot()
	logWeight, err := api.LogWeightSnapshot()
	if err != nil {
		logWeight = 0
	}
	pluginUsage := api.PluginUsageSnapshot()
	logs := monitor.RecentRequestLogs(20)

	var b strings.Builder
	fmt.Fprintf(&b, `<span id="live-sites-count" class="text-4xl font-bold">%d</span>`, len(sites))
	fmt.Fprintf(&b, `<span id="live-vhosts-count" class="text-4xl font-bold">%d</span>`, vhostsCount(sites))
	fmt.Fprintf(&b, `<span id="live-log-weight" class="text-4xl font-bold">%d</span>`, logWeight)
	b.WriteString(renderHomeMetrics(metrics, logWeight, pluginUsage))
	b.WriteString(renderMetrics(metrics, logWeight, pluginUsage))
	b.WriteString(renderSites(sites))
	b.WriteString(renderLogs(logs))
	return b.String()
}

func renderHomeMetrics(metrics map[string]any, logWeight int64, pluginUsage map[string]int) string {
	var b strings.Builder
	b.WriteString(`<ul id="live-home-metrics" class="space-y-2 text-sm text-gray-600">`)
	fmt.Fprintf(&b, `<li>Total Requests: <span>%s</span></li>`, metricValue(metrics["requests_total"]))
	fmt.Fprintf(&b, `<li>Average Latency (ms): <span>%s</span></li>`, metricValue(metrics["latency_avg_ms"]))
	fmt.Fprintf(&b, `<li>CPU Usage (%%): <span>%s</span></li>`, metricValue(metrics["cpu_usage"]))
	fmt.Fprintf(&b, `<li>RAM Usage (MB): <span>%s</span></li>`, metricValue(metrics["ram_usage_mb"]))
	fmt.Fprintf(&b, `<li>Log Files Size: <span>%d</span></li>`, logWeight)
	b.WriteString(`<li>Plugins Usage:<ul class="ml-4">`)
	writePluginUsage(&b, pluginUsage, `<li class="flex justify-between"><span>%s</span><span>%d</span></li>`)
	b.WriteString(`</ul></li></ul>`)
	return b.String()
}

func renderMetrics(metrics map[string]any, logWeight int64, pluginUsage map[string]int) string {
	var b strings.Builder
	b.WriteString(`<ul id="metricsList" class="space-y-2">`)
	metricRows := []struct {
		name  string
		value any
	}{
		{"Requests Total", metrics["requests_total"]},
		{"Average Latency (ms)", metrics["latency_avg_ms"]},
		{"CPU Usage (%)", metrics["cpu_usage"]},
		{"RAM Usage (MB)", metrics["ram_usage_mb"]},
	}
	for _, row := range metricRows {
		fmt.Fprintf(&b, `<li class="flex justify-between border-b py-2"><span class="font-medium">%s</span><span>%s</span></li>`, row.name, metricValue(row.value))
	}
	fmt.Fprintf(&b, `<li class="flex justify-between border-b py-2"><span class="font-medium">Total Log Weight (bytes)</span><span>%d</span></li>`, logWeight)
	b.WriteString(`</ul>`)
	b.WriteString(`<ul id="pluginUsageList" class="space-y-1">`)
	writePluginUsage(&b, pluginUsage, `<li class="flex justify-between border-b py-1"><span>%s</span><span>%d</span></li>`)
	b.WriteString(`</ul>`)
	return b.String()
}

func renderSites(sites []config.SiteConfig) string {
	var b strings.Builder
	b.WriteString(`<ul id="sitesList" class="divide-y divide-gray-200">`)
	for _, site := range sites {
		domain := html.EscapeString(site.Domain)
		mode := "static"
		if site.ProxyPass != "" {
			mode = "proxy"
		}
		tls := "http"
		if site.SSL.Enabled {
			tls = "https"
		}
		fmt.Fprintf(&b, `<li class="px-4 flex items-center justify-between py-3 hover:bg-gray-50 transition-colors duration-200 ease-in-out rounded-lg">`)
		fmt.Fprintf(&b, `<div><p class="font-medium text-gray-700">%s</p><p class="text-sm text-gray-400">Port: %d | %s | %s</p></div>`, domain, site.Port, mode, tls)
		fmt.Fprintf(&b, `<div class="flex items-center space-x-4"><button class="view-json text-blue-600 hover:text-blue-500 text-sm font-medium" data-domain="%s"><span class="material-symbols-outlined"> data_object </span></button><button class="delete-site text-red-500 hover:text-red-400 text-sm font-medium" data-domain="%s"><span class="material-symbols-outlined"> delete </span></button></div>`, domain, domain)
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

func renderLogs(logs []monitor.RequestLog) string {
	var b strings.Builder
	b.WriteString(`<div id="liveLogs" class="space-y-2 text-sm">`)
	if len(logs) == 0 {
		b.WriteString(`<p class="text-gray-400">No requests yet.</p>`)
	} else {
		for i := len(logs) - 1; i >= 0; i-- {
			entry := logs[i]
			domain := html.EscapeString(entry.Domain)
			if domain == "" {
				domain = html.EscapeString(entry.Identifier)
			}
			fmt.Fprintf(&b, `<div class="bg-gray-50 border border-gray-200 rounded p-2 shadow-sm">`)
			fmt.Fprintf(&b, `<div class="text-xs text-gray-500 mb-1"><span class="font-semibold">%s</span> <span>%s</span></div>`, entry.Time.Format("15:04:05"), domain)
			fmt.Fprintf(&b, `<div class="text-gray-800">%s %s %d (%.4fs)</div>`, html.EscapeString(entry.Method), html.EscapeString(entry.URL), entry.StatusCode, entry.DurationSec)
			b.WriteString(`</div>`)
		}
	}
	b.WriteString(`</div>`)
	return b.String()
}

func writePluginUsage(b *strings.Builder, usage map[string]int, format string) {
	names := make([]string, 0, len(usage))
	for name := range usage {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Fprintf(b, format, "none", 0)
		return
	}
	for _, name := range names {
		fmt.Fprintf(b, format, html.EscapeString(name), usage[name])
	}
}

func metricValue(value any) string {
	switch v := value.(type) {
	case []float64:
		if len(v) == 0 {
			return "0"
		}
		return fmt.Sprintf("%.2f", v[0])
	case float64:
		return fmt.Sprintf("%.2f", v)
	case uint64:
		return fmt.Sprintf("%d", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	default:
		return html.EscapeString(fmt.Sprint(v))
	}
}

func vhostsCount(sites []config.SiteConfig) int {
	ports := make(map[int]int)
	for _, site := range sites {
		ports[site.Port]++
	}
	count := 0
	for _, portCount := range ports {
		if portCount > 1 {
			count++
		}
	}
	return count
}
