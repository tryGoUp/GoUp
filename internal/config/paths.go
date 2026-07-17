package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateDomain rejects domain names that are empty or contain characters
// that could escape the configuration directory when used to build a file
// path (path separators, "..", NUL, etc.). It accepts standard hostnames and
// wildcard labels only.
func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is empty")
	}
	if len(domain) > 253 {
		return fmt.Errorf("domain is too long")
	}
	if strings.ContainsAny(domain, "/\\\x00") {
		return fmt.Errorf("domain contains an invalid character")
	}
	if strings.Contains(domain, "..") {
		return fmt.Errorf("domain contains '..'")
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" {
			// Allows neither a leading/trailing dot nor an empty label.
			continue
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '-' || r == '_' || r == '*':
			default:
				return fmt.Errorf("domain contains an invalid character")
			}
		}
	}
	return nil
}

// SiteConfigPath returns the on-disk path for a site's configuration file,
// guaranteeing the result stays inside the configuration directory.
func SiteConfigPath(domain string) (string, error) {
	if err := ValidateDomain(domain); err != nil {
		return "", err
	}
	return SafeJoin(GetConfigDir(), domain+".json")
}

// CheckPath validates an operator-supplied filesystem path and reports whether
// it exists. It rejects relative paths and any path containing a ".." traversal
// segment before touching the filesystem, so request- or config-derived data is
// sanitised before it reaches os.Stat. invalid is true when the path fails
// validation (as opposed to simply not existing).
func CheckPath(p string) (exists bool, invalid bool) {
	if p == "" {
		return false, false
	}
	if strings.Contains(p, "..") || !filepath.IsAbs(p) {
		return false, true
	}
	if _, err := os.Stat(p); err == nil {
		return true, false
	}
	return false, false
}

// SafeJoin joins base with an untrusted relative path and verifies that the
// result does not escape base. It is the guard used for every file path built
// from request- or config-supplied data.
func SafeJoin(base, rel string) (string, error) {
	cleanRel := filepath.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))
	cleanRel = strings.TrimPrefix(cleanRel, "/")
	joined := filepath.Join(base, cleanRel)

	rel, err := filepath.Rel(base, joined)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return joined, nil
}
