package config

import "testing"

func TestSiteConfigValidate(t *testing.T) {
	cases := []struct {
		name     string
		conf     SiteConfig
		wantErrs bool
	}{
		{
			name:     "missing target",
			conf:     SiteConfig{Domain: "example.com", Port: 80},
			wantErrs: true,
		},
		{
			name:     "bad port",
			conf:     SiteConfig{Domain: "example.com", Port: 0, ProxyPass: "http://localhost:3000"},
			wantErrs: true,
		},
		{
			name:     "bad proxy url",
			conf:     SiteConfig{Domain: "example.com", Port: 80, ProxyPass: "not-a-url"},
			wantErrs: true,
		},
		{
			name:     "invalid upstream",
			conf:     SiteConfig{Domain: "example.com", Port: 80, ProxyUpstreams: []string{"http://ok:1", "nope"}},
			wantErrs: true,
		},
		{
			name:     "ssl without cert",
			conf:     SiteConfig{Domain: "example.com", Port: 443, ProxyPass: "http://localhost:3000", SSL: SSLConfig{Enabled: true}},
			wantErrs: true,
		},
		{
			name:     "bad flush interval",
			conf:     SiteConfig{Domain: "example.com", Port: 80, ProxyPass: "http://localhost:3000", FlushInterval: "notaduration"},
			wantErrs: true,
		},
		{
			name:     "valid proxy site",
			conf:     SiteConfig{Domain: "example.com", Port: 80, ProxyPass: "http://localhost:3000"},
			wantErrs: false,
		},
		{
			name:     "valid load-balanced site",
			conf:     SiteConfig{Domain: "example.com", Port: 80, ProxyUpstreams: []string{"http://a:1", "http://b:2"}},
			wantErrs: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := c.conf.Validate()
			if c.wantErrs && len(errs) == 0 {
				t.Errorf("expected validation errors, got none")
			}
			if !c.wantErrs && len(errs) != 0 {
				t.Errorf("expected no errors, got %v", errs)
			}
		})
	}
}
