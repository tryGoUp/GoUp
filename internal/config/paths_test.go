package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDomain(t *testing.T) {
	valid := []string{"example.com", "a.local", "sub.example.co.uk", "www_test-1.example.com", "*.example.com"}
	for _, d := range valid {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("expected %q to be valid, got %v", d, err)
		}
	}

	invalid := []string{
		"",
		"../../etc/passwd",
		"..",
		"a/b",
		"a\\b",
		"a\x00b",
		"foo/../bar",
	}
	for _, d := range invalid {
		if err := ValidateDomain(d); err == nil {
			t.Errorf("expected %q to be rejected", d)
		}
	}
}

func TestSiteConfigPathRejectsTraversal(t *testing.T) {
	if _, err := SiteConfigPath("../../../tmp/evil"); err == nil {
		t.Fatal("expected traversal domain to be rejected")
	}
	p, err := SiteConfigPath("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("goup", "example.com.json")) {
		t.Errorf("unexpected path: %s", p)
	}
}

func TestSafeJoinContainment(t *testing.T) {
	base := filepath.Join("/var", "log", "goup")

	// SafeJoin clamps traversal back inside base rather than escaping it, so the
	// result must always stay under base (a request for ../../etc/passwd hits a
	// non-existent file inside the log dir, never /etc/passwd).
	for _, rel := range []string{"../../etc/passwd", "sub/../../../etc/passwd", "/../../etc/passwd"} {
		got, err := SafeJoin(base, rel)
		if err != nil {
			continue // rejecting is also acceptable
		}
		if got != base && !strings.HasPrefix(got, base+string(filepath.Separator)) {
			t.Errorf("SafeJoin(%q, %q) = %q escaped base %q", base, rel, got, base)
		}
	}

	got, err := SafeJoin(base, "2026/07/16.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(base, "2026", "07", "16.log")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
