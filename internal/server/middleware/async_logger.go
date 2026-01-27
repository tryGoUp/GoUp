package middleware

import (
	"sync"

	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/tui"
)

// LogEntry represents a single log event.
type LogEntry struct {
	Logger     *logger.Logger
	Fields     logger.Fields
	Message    string
	Identifier string
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
				return &LogEntry{
					Fields: make(logger.Fields),
				}
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
	entry.Logger = nil
	entry.Message = ""
	entry.Identifier = ""
	for k := range entry.Fields {
		delete(entry.Fields, k)
	}
	al.logEntryPool.Put(entry)
}

// worker processes log entries from the channel.
func (al *AsyncLogger) worker() {
	for entry := range al.logChan {
		entry.Logger.WithFields(entry.Fields).Info(entry.Message)

		if tui.IsEnabled() {
			tui.UpdateLog(entry.Identifier, entry.Fields)
		}

		al.PutEntry(entry)
	}
}

// Shutdown closes the log channel.
func (al *AsyncLogger) Shutdown() {
	close(al.logChan)
}
