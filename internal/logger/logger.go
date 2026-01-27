package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/muesli/termenv"
	"github.com/rs/zerolog"
)

var loggerBytePool = &byteSlicePool{
	pool: sync.Pool{
		New: func() any {
			return make([]byte, 8*1024)
		},
	},
}

type byteSlicePool struct {
	pool sync.Pool
}

func (b *byteSlicePool) Get() []byte {
	return b.pool.Get().([]byte)
}

func (b *byteSlicePool) Put(buf []byte) {
	b.pool.Put(buf)
}

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
	logDir := filepath.Join(
		config.GetLogDir(),
		identifier,
		fmt.Sprintf("%d", time.Now().Year()),
		fmt.Sprintf("%02d", time.Now().Month()),
	)
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		return nil, err
	}

	// Log file name format: 03.log (day.log)
	logFile := filepath.Join(logDir, fmt.Sprintf("%02d.log", time.Now().Day()))
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
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
	logDir := filepath.Join(
		config.GetLogDir(),
		siteDomain,
		fmt.Sprintf("%d", time.Now().Year()),
		fmt.Sprintf("%02d", time.Now().Month()),
	)
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		return nil, err
	}

	// Log file format: 03-NomePlugin.log (day-PluginName.log)
	logFile := filepath.Join(logDir, fmt.Sprintf("%02d-%s.log", time.Now().Day(), pluginName))
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
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

// Writer returns an io.WriteCloser that logs each written line.
func (l *Logger) Writer() io.WriteCloser {
	pr, pw := io.Pipe()

	go func() {
		defer pr.Close()
		buf := loggerBytePool.Get()
		defer loggerBytePool.Put(buf)

		var tmp []byte

		for {
			n, err := pr.Read(buf)
			if n > 0 {
				tmp = append(tmp, buf[:n]...)
				for {
					idx := indexOfNewline(tmp)
					if idx == -1 {
						break
					}
					line := tmp[:idx]
					line = trimCR(line)
					l.Info(string(line))
					tmp = tmp[idx+1:]
				}
			}
			if err != nil {
				// Exit on error or EOF
				break
			}
		}
		// Logging any remaining data
		if len(tmp) > 0 {
			l.Info(string(tmp))
		}
	}()

	return &pipeWriteCloser{pipeWriter: pw}
}

// pipeWriteCloser implements Write and Close delegating to a PipeWriter.
type pipeWriteCloser struct {
	pipeWriter *io.PipeWriter
}

func (pwc *pipeWriteCloser) Write(data []byte) (int, error) {
	return pwc.pipeWriter.Write(data)
}

func (pwc *pipeWriteCloser) Close() error {
	return pwc.pipeWriter.Close()
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
