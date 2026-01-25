package dns

import (
	"net"
	"testing"

	"github.com/miekg/dns"
	"github.com/mirkobrombin/goup/internal/config"
)

func TestDNSHandler_ServeDNS(t *testing.T) {
	// Setup mock config
	conf := &config.DNSConfig{
		Enable: true,
		Zones: map[string][]config.DNSRecord{
			"example.com": {
				{Type: "A", Name: "@", Value: "1.2.3.4", TTL: 3600},
				{Type: "CNAME", Name: "www", Value: "@", TTL: 3600},
				{Type: "TXT", Name: "@", Value: "v=spf1 -all", TTL: 3600},
			},
		},
	}

	handler, err := NewDNSHandler(conf)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	tests := []struct {
		name     string
		qname    string
		qtype    uint16
		wantCode int
		wantAns  int
	}{
		{
			name:     "A record for apex",
			qname:    "example.com.",
			qtype:    dns.TypeA,
			wantCode: dns.RcodeSuccess,
			wantAns:  1,
		},
		{
			name:     "TXT record for apex",
			qname:    "example.com.",
			qtype:    dns.TypeTXT,
			wantCode: dns.RcodeSuccess,
			wantAns:  1,
		},
		{
			name:     "CNAME for www",
			qname:    "www.example.com.",
			qtype:    dns.TypeA,
			wantCode: dns.RcodeSuccess,
			wantAns:  1,
		},
		{
			name:     "NXDOMAIN for non-existent",
			qname:    "foo.example.com.",
			qtype:    dns.TypeA,
			wantCode: dns.RcodeSuccess,
			wantAns:  0,
		},
		{
			name:     "Zone mismatch (Upstream not set)",
			qname:    "google.com.",
			qtype:    dns.TypeA,
			wantCode: dns.RcodeNameError,
			wantAns:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &mockResponseWriter{}
			req := new(dns.Msg)
			req.SetQuestion(tt.qname, tt.qtype)

			handler.ServeDNS(w, req)

			if w.msg.Rcode != tt.wantCode {
				t.Errorf("got Rcode %v, want %v", w.msg.Rcode, tt.wantCode)
			}
			if len(w.msg.Answer) != tt.wantAns {
				t.Errorf("got %d answers, want %d", len(w.msg.Answer), tt.wantAns)
			}

			if tt.wantAns > 0 {
				if tt.qtype == dns.TypeA && tt.qname == "example.com." {
					a, ok := w.msg.Answer[0].(*dns.A)
					if !ok {
						t.Errorf("Answer is not A record")
					} else if a.A.String() != "1.2.3.4" {
						t.Errorf("Got A %s, want 1.2.3.4", a.A.String())
					}
				}
			}
		})
	}
}

type mockResponseWriter struct {
	msg *dns.Msg
}

func (m *mockResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53}
}
func (m *mockResponseWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}
func (m *mockResponseWriter) WriteMsg(msg *dns.Msg) error {
	m.msg = msg
	return nil
}
func (m *mockResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (m *mockResponseWriter) Close() error              { return nil }
func (m *mockResponseWriter) TsigStatus() error         { return nil }
func (m *mockResponseWriter) TsigTimersOnly(bool)       {}
func (m *mockResponseWriter) Hijack()                   {}
