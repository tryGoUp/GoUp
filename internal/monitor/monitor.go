package monitor

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/mirkobrombin/goup/internal/logger"
)

type RequestLog struct {
	Time        time.Time
	Identifier  string
	Domain      string
	Method      string
	URL         string
	StatusCode  int
	DurationSec float64
}

const maxRequestLogs = 100

var requestCount uint64

var requestLogs = struct {
	sync.RWMutex
	items []RequestLog
}{}

func AddRequestLog(identifier string, fields logger.Fields) {
	atomic.AddUint64(&requestCount, 1)
	entry := RequestLog{
		Time:       time.Now(),
		Identifier: identifier,
	}
	entry.Domain, _ = fields["domain"].(string)
	entry.Method, _ = fields["method"].(string)
	entry.URL, _ = fields["url"].(string)
	entry.StatusCode, _ = fields["status_code"].(int)
	entry.DurationSec, _ = fields["duration_sec"].(float64)

	requestLogs.Lock()
	requestLogs.items = append(requestLogs.items, entry)
	if len(requestLogs.items) > maxRequestLogs {
		requestLogs.items = requestLogs.items[len(requestLogs.items)-maxRequestLogs:]
	}
	requestLogs.Unlock()
}

func RecentRequestLogs(limit int) []RequestLog {
	requestLogs.RLock()
	defer requestLogs.RUnlock()

	if limit <= 0 || limit > len(requestLogs.items) {
		limit = len(requestLogs.items)
	}

	start := len(requestLogs.items) - limit
	items := make([]RequestLog, limit)
	copy(items, requestLogs.items[start:])
	return items
}

func RequestCount() uint64 {
	return atomic.LoadUint64(&requestCount)
}
