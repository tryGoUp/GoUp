package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
)

// rotatingFileWriter writes log lines to a date-partitioned file and reopens a
// new file when the calendar day changes. A long-running process therefore
// keeps writing into the correct YYYY/MM/DD.log instead of the file that was
// current when the logger was first created.
type rotatingFileWriter struct {
	mu         sync.Mutex
	identifier string
	pluginName string // empty for the main access logger
	file       *os.File
	curKey     string
}

func newRotatingFileWriter(identifier, pluginName string) (*rotatingFileWriter, error) {
	w := &rotatingFileWriter{identifier: identifier, pluginName: pluginName}
	if err := w.rotateIfNeeded(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingFileWriter) rotateIfNeeded() error {
	now := time.Now()
	key := fmt.Sprintf("%04d-%02d-%02d", now.Year(), now.Month(), now.Day())
	if w.file != nil && key == w.curKey {
		return nil
	}

	dir := filepath.Join(
		config.GetLogDir(),
		w.identifier,
		fmt.Sprintf("%d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
	)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	var name string
	if w.pluginName == "" {
		name = fmt.Sprintf("%02d.log", now.Day())
	} else {
		name = fmt.Sprintf("%02d-%s.log", now.Day(), w.pluginName)
	}

	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return err
	}
	if w.file != nil {
		_ = w.file.Close()
	}
	w.file = f
	w.curKey = key
	return nil
}

func (w *rotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateIfNeeded(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *rotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
