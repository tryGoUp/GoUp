// Package logger provides GoUp's logging facade. It is implemented on top of
// go-foundation v2 core/logger: zerolog is gone, entries flow through
// foundation sinks (a JSON file sink on the rotating writer and a colored
// console sink for stdout).
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/logger"
	"github.com/muesli/termenv"
)

// Fields is a map of string keys to arbitrary values, emulating logrus.Fields
// for compatibility with existing code.
type Fields map[string]any

// Logger wraps a foundation logger while exposing methods similar to logrus.
type Logger struct {
	base   logger.Logger
	fields Fields
	mu     sync.Mutex
	sinks  []logger.Sink
}

func toFields(f Fields) []logger.Field {
	if len(f) == 0 {
		return nil
	}
	out := make([]logger.Field, 0, len(f))
	for k, v := range f {
		out = append(out, logger.Field{Key: k, Value: v})
	}
	return out
}

// SetOutput replaces the sinks with a single JSON writer sink.
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rebuild([]logger.Sink{&jsonWriterSink{w: w}})
}

func (l *Logger) rebuild(sinks []logger.Sink) {
	l.sinks = sinks
	opts := []logger.Option{logger.WithoutDefaultSink()}
	for _, s := range sinks {
		opts = append(opts, logger.WithSink(s))
	}
	l.base = logger.New(opts...).With(toFields(l.fields)...)
}

// WithFields returns a new Logger that includes the provided fields.
func (l *Logger) WithFields(fields Fields) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	merged := Fields{}
	for k, v := range l.fields {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	nl := &Logger{fields: merged}
	nl.rebuild(l.sinks)
	return nl
}

// jsonWriterSink writes each entry as one JSON object per line.
type jsonWriterSink struct {
	w  io.Writer
	mu sync.Mutex
}

func (s *jsonWriterSink) Log(e logger.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fields := make(map[string]any, len(e.Fields)+3)
	for k, v := range e.Fields {
		fields[k] = v
	}
	fields["level"] = e.Level
	fields["time"] = e.Time.Format(time.RFC3339)
	fields["msg"] = e.Msg
	out, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = s.w.Write(append(out, '\n'))
	return err
}

// consoleSink prints colored, human-readable lines to an io.Writer.
type consoleSink struct {
	w io.Writer
}

var levelColors = map[string]termenv.Color{
	"info":  termenv.ANSICyan,
	"warn":  termenv.ANSIYellow,
	"error": termenv.ANSIRed,
	"debug": termenv.ANSIWhite,
}

func (s *consoleSink) Log(e logger.Entry) error {
	label := strings.ToUpper(e.Level)
	if c, ok := levelColors[e.Level]; ok {
		label = termenv.String(label).Foreground(c).String()
	}
	line := fmt.Sprintf("%s %s %s", e.Time.Format("15:04:05"), label, e.Msg)
	for k, v := range e.Fields {
		line += fmt.Sprintf(" %s=%v", k, v)
	}
	_, err := fmt.Fprintln(s.w, line)
	return err
}

// Info logs a message at Info level.
func (l *Logger) Info(msg string) { l.base.Info(msg) }

// Infof logs a formatted message at Info level.
func (l *Logger) Infof(format string, args ...any) { l.base.Info(fmt.Sprintf(format, args...)) }

// Error logs a message at Error level.
func (l *Logger) Error(msg string) { l.base.Error(msg) }

// Errorf logs a formatted message at Error level.
func (l *Logger) Errorf(format string, args ...any) { l.base.Error(fmt.Sprintf(format, args...)) }

// Debug logs a message at Debug level.
func (l *Logger) Debug(msg string) { l.base.Debug(msg) }

// Debugf logs a formatted message at Debug level.
func (l *Logger) Debugf(format string, args ...any) { l.base.Debug(fmt.Sprintf(format, args...)) }

// Warn logs a message at Warn level.
func (l *Logger) Warn(msg string) { l.base.Warn(msg) }

// Warnf logs a formatted message at Warn level.
func (l *Logger) Warnf(format string, args ...any) { l.base.Warn(fmt.Sprintf(format, args...)) }

// NewLogger creates a new Logger that writes JSON to files and colored
// logs to stdout.
func NewLogger(identifier string, fields Fields) (*Logger, error) {
	// A date-partitioned writer that reopens the file when the day rolls over,
	// so a process running past midnight keeps writing to the right day's log.
	file, err := newRotatingFileWriter(identifier, "")
	if err != nil {
		return nil, err
	}

	l := &Logger{fields: fields}
	l.rebuild([]logger.Sink{&jsonWriterSink{w: file}, &consoleSink{w: os.Stdout}})
	return l, nil
}

// NewPluginLogger creates a plugin-specific log file (JSON format, no stdout).
func NewPluginLogger(siteDomain, pluginName string) (*Logger, error) {
	// Date-partitioned, day-rotating writer (day-PluginName.log).
	file, err := newRotatingFileWriter(siteDomain, pluginName)
	if err != nil {
		return nil, err
	}

	l := &Logger{}
	l.rebuild([]logger.Sink{&jsonWriterSink{w: file}})
	return l, nil
}

// NewSystemLogger creates a system-level log file (e.g. for SafeGuard).
// It stores logs in logs/system/YYYY/MM/DD-Name.log.
func NewSystemLogger(name string) (*Logger, error) {
	return NewPluginLogger("system", name)
}

// Writer returns an io.WriteCloser that logs each complete line written to it.
//
// It writes directly and synchronously, with no background goroutine or pipe.
// The previous implementation spawned a goroutine blocked on an io.Pipe read;
// because os/exec never closes a user-supplied writer, that goroutine (and its
// pooled buffer) leaked for every spawned child process.
func (l *Logger) Writer() io.WriteCloser {
	return &lineWriter{l: l}
}

// lineWriter accumulates bytes and emits one log line per newline. Each stream
// (stdout/stderr) gets its own instance and is written by a single os/exec copy
// goroutine, so no internal locking is required.
type lineWriter struct {
	l   *Logger
	buf []byte
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.buf = append(lw.buf, p...)
	for {
		idx := indexOfNewline(lw.buf)
		if idx == -1 {
			break
		}
		lw.l.Info(string(trimCR(lw.buf[:idx])))
		lw.buf = lw.buf[idx+1:]
	}
	return len(p), nil
}

func (lw *lineWriter) Close() error {
	if len(lw.buf) > 0 {
		lw.l.Info(string(trimCR(lw.buf)))
		lw.buf = nil
	}
	return nil
}

func indexOfNewline(buf []byte) int {
	for i, b := range buf {
		if b == '\n' {
			return i
		}
	}
	return -1
}

func trimCR(buf []byte) []byte {
	if len(buf) > 0 && buf[len(buf)-1] == '\r' {
		return buf[:len(buf)-1]
	}
	return buf
}
