package middleware

import (
	"sync"

	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/monitor"
	"github.com/mirkobrombin/goup/internal/tui"
)

// LogEntry represents a single access-log event. Fields are typed so the
// request hot path does not box values into a map; the worker builds the
// map once, off the request path.
type LogEntry struct {
	Logger     *logger.Logger
	Message    string
	Identifier string

	Method     string
	URL        string
	RemoteAddr string
	Domain     string
	StatusCode int
	Duration   float64
}

// AsyncLogger handles logging asynchronously.
type AsyncLogger struct {
	logChan      chan *LogEntry
	logEntryPool sync.Pool
}

var globalAsyncLogger *AsyncLogger

// InitAsyncLogger initializes the global async logger.
func InitAsyncLogger(bufferSize int) {
	globalAsyncLogger = &AsyncLogger{
		logChan: make(chan *LogEntry, bufferSize),
		logEntryPool: sync.Pool{
			New: func() any {
				return &LogEntry{}
			},
		},
	}
	go globalAsyncLogger.worker()
}

// GetAsyncLogger returns the global async logger instance.
func GetAsyncLogger() *AsyncLogger {
	return globalAsyncLogger
}

// GetEntry retrieves a LogEntry from the pool.
func (al *AsyncLogger) GetEntry() *LogEntry {
	return al.logEntryPool.Get().(*LogEntry)
}

// Log queues a log entry. If the buffer is full, the log is dropped
// and the entry is returned to the pool immediately.
func (al *AsyncLogger) Log(entry *LogEntry) {
	select {
	case al.logChan <- entry:
	default:
		// Drop log if buffer is full to maintain performance
		al.PutEntry(entry)
	}
}

// PutEntry resets and returns a LogEntry to the pool.
func (al *AsyncLogger) PutEntry(entry *LogEntry) {
	*entry = LogEntry{}
	al.logEntryPool.Put(entry)
}

// worker processes log entries from the channel. It is the only goroutine
// touching the scratch map, which is rebuilt per entry without reallocating.
func (al *AsyncLogger) worker() {
	fields := make(logger.Fields, 8)
	for entry := range al.logChan {
		for k := range fields {
			delete(fields, k)
		}
		fields["method"] = entry.Method
		fields["url"] = entry.URL
		fields["remote_addr"] = entry.RemoteAddr
		fields["status_code"] = entry.StatusCode
		fields["duration_sec"] = entry.Duration
		fields["domain"] = entry.Domain

		entry.Logger.WithFields(fields).Info(entry.Message)
		monitor.AddRequestLog(entry.Identifier, fields)

		if tui.IsEnabled() {
			tui.UpdateLog(entry.Identifier, fields)
		}

		al.PutEntry(entry)
	}
}

// Shutdown closes the log channel.
func (al *AsyncLogger) Shutdown() {
	close(al.logChan)
}
