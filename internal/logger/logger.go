package logger

import (
	"io"
	"os"

	"github.com/muesli/termenv"
	"github.com/rs/zerolog"
)

// Fields is a map of string keys to arbitrary values, emulating logrus.Fields
// for compatibility with existing code.
type Fields map[string]any

// Logger wraps a zerolog.Logger while exposing methods similar to logrus.
type Logger struct {
	base zerolog.Logger
	out  io.Writer
}

// SetOutput changes the output writer (stdout, file, etc.).
func (l *Logger) SetOutput(w io.Writer) {
	l.out = w
	l.base = l.base.Output(w)
}

// WithFields returns a new Logger that includes the provided fields.
func (l *Logger) WithFields(fields Fields) *Logger {
	newBase := l.base.With().Fields(fields).Logger()
	return &Logger{
		base: newBase,
		out:  l.out,
	}
}

// ColoredConsoleWriter wraps an io.Writer to print colored logs.
func ColoredConsoleWriter(out io.Writer) zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{
		Out:        out,
		TimeFormat: "15:04:05",
		FormatLevel: func(i any) string {
			level := i.(string)
			switch level {
			case "info":
				return termenv.String("INFO").Foreground(termenv.ANSICyan).String()
			case "warn":
				return termenv.String("WARN").Foreground(termenv.ANSIYellow).String()
			case "error":
				return termenv.String("ERROR").Foreground(termenv.ANSIRed).String()
			case "debug":
				return termenv.String("DEBUG").Foreground(termenv.ANSIWhite).String()
			default:
				return level
			}
		},
	}
}

// Info logs a message at Info level.
func (l *Logger) Info(msg string) {
	l.base.Info().Msg(msg)
}

// Infof logs a formatted message at Info level.
func (l *Logger) Infof(format string, args ...any) {
	l.base.Info().Msgf(format, args...)
}

// Error logs a message at Error level.
func (l *Logger) Error(msg string) {
	l.base.Error().Msg(msg)
}

// Errorf logs a formatted message at Error level.
func (l *Logger) Errorf(format string, args ...any) {
	l.base.Error().Msgf(format, args...)
}

// Debug logs a message at Debug level.
func (l *Logger) Debug(msg string) {
	l.base.Debug().Msg(msg)
}

// Debugf logs a formatted message at Debug level.
func (l *Logger) Debugf(format string, args ...any) {
	l.base.Debug().Msgf(format, args...)
}

// Warn logs a message at Warn level.
func (l *Logger) Warn(msg string) {
	l.base.Warn().Msg(msg)
}

// Warnf logs a formatted message at Warn level.
func (l *Logger) Warnf(format string, args ...any) {
	l.base.Warn().Msgf(format, args...)
}

// NewLogger creates a new Logger that writes JSON to files and colored
// logs to stdout.
func NewLogger(identifier string, fields Fields) (*Logger, error) {
	// A date-partitioned writer that reopens the file when the day rolls over,
	// so a process running past midnight keeps writing to the right day's log.
	file, err := newRotatingFileWriter(identifier, "")
	if err != nil {
		return nil, err
	}

	multiWriter := io.MultiWriter(file, ColoredConsoleWriter(os.Stdout))

	base := zerolog.New(multiWriter).With().Timestamp().Logger()

	l := &Logger{
		base: base,
		out:  multiWriter,
	}

	if fields != nil {
		l = l.WithFields(fields)
	}

	return l, nil
}

// NewPluginLogger creates a plugin-specific log file (JSON format, no stdout).
func NewPluginLogger(siteDomain, pluginName string) (*Logger, error) {
	// Date-partitioned, day-rotating writer (day-PluginName.log).
	file, err := newRotatingFileWriter(siteDomain, pluginName)
	if err != nil {
		return nil, err
	}

	// Only file output with timestamp
	base := zerolog.New(file).With().Timestamp().Logger()

	l := &Logger{
		base: base,
		out:  file,
	}
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
