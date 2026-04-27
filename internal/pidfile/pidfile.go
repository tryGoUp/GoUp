package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func Path() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine cache dir: %w", err)
	}
	goupDir := filepath.Join(dir, "goup")
	if err := os.MkdirAll(goupDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create goup dir: %w", err)
	}
	return filepath.Join(goupDir, "goup.pid"), nil
}

func Write() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}

func Read() (int, error) {
	path, err := Path()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("goup is not running (no pid file found)")
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file content: %s", string(data))
	}
	return pid, nil
}

func Remove() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return os.Remove(path)
}
