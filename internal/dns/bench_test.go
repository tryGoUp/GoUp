package dns

import (
	"fmt"
	"testing"

	"github.com/miekg/dns"
	"github.com/mirkobrombin/goup/internal/config"
)

func benchHandler(b *testing.B, hosts int) *DNSHandler {
	b.Helper()
	records := []config.DNSRecord{
		{Name: "@", Type: "A", Value: "192.0.2.1", TTL: 300},
		{Name: "www", Type: "CNAME", Value: "bench.test", TTL: 300},
	}
	for i := 0; i < hosts; i++ {
		records = append(records, config.DNSRecord{
			Name:  fmt.Sprintf("host-%d", i),
			Type:  "A",
			Value: "192.0.2.10",
			TTL:   300,
		})
	}
	conf := &config.DNSConfig{
		Zones: map[string][]config.DNSRecord{"bench.test": records},
	}
	h, err := NewDNSHandler(conf)
	if err != nil {
		b.Fatal(err)
	}
	return h
}

func benchQuery(b *testing.B, qname string, qtype uint16) {
	h := benchHandler(b, 200)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := new(dns.Msg)
		m.SetQuestion(qname, qtype)
		w := &mockResponseWriter{}
		h.ServeDNS(w, m)
	}
}

func BenchmarkServeDNS_A(b *testing.B)        { benchQuery(b, "host-150.bench.test.", dns.TypeA) }
func BenchmarkServeDNS_CNAME(b *testing.B)    { benchQuery(b, "www.bench.test.", dns.TypeA) }
func BenchmarkServeDNS_NXDOMAIN(b *testing.B) { benchQuery(b, "missing.bench.test.", dns.TypeA) }
