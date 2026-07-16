// file: internal/api/logs_explorer.go
package api

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mirkobrombin/goup/internal/config"
)

// LogFileInfo holds data about a single log file.
type LogFileInfo struct {
	Domain    string `json:"domain"`
	Plugin    string `json:"plugin,omitempty"`
	Year      int    `json:"year"`
	Month     int    `json:"month"`
	Day       int    `json:"day"`
	FileName  string `json:"file_name"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   int64  `json:"mod_time_unix"`
}

// GET /api/logfiles?start=YYYY-MM-DD&end=YYYY-MM-DD&plugin=somePlugin
// Lists all log files, optionally filtered by date range or plugin name.
func listLogFilesHandler(w http.ResponseWriter, r *http.Request) {
	startStr := r.URL.Query().Get("start") // YYYY-MM-DD
	endStr := r.URL.Query().Get("end")     // YYYY-MM-DD
	pluginQ := r.URL.Query().Get("plugin") // plugin name

	var startTime, endTime time.Time
	var err error

	if startStr != "" {
		startTime, err = time.Parse("2006-01-02", startStr)
		if err != nil {
			http.Error(w, "Invalid 'start' date format (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
	}
	if endStr != "" {
		endTime, err = time.Parse("2006-01-02", endStr)
		if err != nil {
			http.Error(w, "Invalid 'end' date format (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
	}

	rootDir := config.GetLogDir()
	var results []LogFileInfo

	_ = filepath.Walk(rootDir, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(rootDir, path)
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) != 4 {
			return nil
		}
		domain := parts[0]
		yearStr := parts[1]
		monthStr := parts[2]
		fileName := parts[3] // e.g. "05.log" or "05-something.log"

		year, yErr := strconv.Atoi(yearStr)
		month, mErr := strconv.Atoi(monthStr)
		if yErr != nil || mErr != nil {
			return nil
		}

		// Parse day and optional plugin from the file name
		// e.g. "05.log" -> day=05, plugin=""
		// or   "05-MyPlugin.log" -> day=05, plugin="MyPlugin"
		dayStr, pluginName := parseDayAndPlugin(fileName)
		day, dErr := strconv.Atoi(dayStr)
		if dErr != nil {
			return nil
		}

		fileDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

		// Filter by date if set
		if !startTime.IsZero() && fileDate.Before(startTime) {
			return nil
		}
		if !endTime.IsZero() && fileDate.After(endTime) {
			return nil
		}
		if pluginQ != "" && pluginName != pluginQ {
			return nil
		}

		results = append(results, LogFileInfo{
			Domain:    domain,
			Plugin:    pluginName,
			Year:      year,
			Month:     month,
			Day:       day,
			FileName:  rel,
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().Unix(),
		})
		return nil
	})

	jsonResponse(w, results)
}

// GET /api/logfiles/{fileName}
// Returns the content of a log file.
func getLogFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileName := vars["fileName"]

	if fileName == "" {
		http.Error(w, "fileName is required", http.StatusBadRequest)
		return
	}
	fullPath, err := config.SafeJoin(config.GetLogDir(), fileName)
	if err != nil {
		http.Error(w, "Invalid log file path", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "Log file not found", http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "Failed to read log file", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(data)
}

// parseDayAndPlugin extracts the day and optional plugin name from a log
// file name.
func parseDayAndPlugin(fileName string) (string, string) {
	base := strings.TrimSuffix(fileName, ".log")
	parts := strings.SplitN(base, "-", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
