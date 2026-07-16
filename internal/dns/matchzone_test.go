package dns

import (
	"testing"

	"github.com/mirkobrombin/goup/internal/config"
)

// TestMatchZoneLabelBoundary guards against the suffix-match bug where a query
// for a lookalike domain (evilexample.com) would be answered authoritatively
// for the zone example.com.
func TestMatchZoneLabelBoundary(t *testing.T) {
	conf := &config.DNSConfig{
		Enable: true,
		Zones: map[string][]config.DNSRecord{
			"example.com": {{Type: "A", Name: "@", Value: "1.2.3.4", TTL: 3600}},
		},
	}
	h, err := NewDNSHandler(conf)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	cases := []struct {
		name string
		want string
	}{
		{"example.com.", "example.com"},
		{"www.example.com.", "example.com"},
		{"evilexample.com.", ""},
		{"notexample.com.", ""},
		{"example.com.evil.com.", ""},
	}
	for _, c := range cases {
		if got := h.matchZone(c.name); got != c.want {
			t.Errorf("matchZone(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
