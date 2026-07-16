package dns

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
)

// DNSHandler implements the dns.Handler interface. Records are indexed and
// pre-compiled into dns.RR values at construction time, so serving a query is
// two map lookups instead of a linear scan plus per-query record parsing.
type DNSHandler struct {
	Config *config.DNSConfig
	Logger *logger.Logger

	// zones is sorted by label length (longest first) so overlapping zones
	// (e.g. "sub.example.com" inside "example.com") match deterministically.
	zones []string
	// rrIndex maps lowercase FQDN -> qtype -> pre-built answers.
	rrIndex map[string]map[uint16][]dns.RR
	// cnameIndex maps lowercase FQDN -> pre-built CNAME answer, returned for
	// any query type on that name (RFC 1034).
	cnameIndex map[string][]dns.RR
	// names records every FQDN that exists in a zone, for NXDOMAIN vs NODATA.
	names map[string]bool

	// allowRecursion lists the networks permitted to use the forwarder.
	allowRecursion []*net.IPNet

	client *dns.Client
}

// NewDNSHandler creates a new DNS handler.
func NewDNSHandler(conf *config.DNSConfig) (*DNSHandler, error) {
	l, err := logger.NewSystemLogger("DNS")
	if err != nil {
		return nil, err
	}
	h := &DNSHandler{
		Config:     conf,
		Logger:     l,
		rrIndex:    make(map[string]map[uint16][]dns.RR),
		cnameIndex: make(map[string][]dns.RR),
		names:      make(map[string]bool),
		client:     &dns.Client{Timeout: 5 * time.Second},
	}
	h.buildIndex()
	h.allowRecursion = buildRecursionACL(conf.AllowRecursionFrom, l)
	return h, nil
}

// buildRecursionACL parses the configured CIDRs. When none are configured it
// defaults to loopback and RFC1918/ULA private ranges, so the forwarder is
// never open to the public Internet by default.
func buildRecursionACL(cidrs []string, l *logger.Logger) []*net.IPNet {
	if len(cidrs) == 0 {
		cidrs = []string{
			"127.0.0.0/8", "::1/128",
			"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
			"fc00::/7", "fe80::/10",
		}
	}
	var nets []*net.IPNet
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			l.Errorf("Invalid allow_recursion_from CIDR %q: %v", c, err)
			continue
		}
		nets = append(nets, n)
	}
	return nets
}

// recursionAllowed reports whether the client address may use the forwarder.
func (h *DNSHandler) recursionAllowed(addr net.Addr) bool {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range h.allowRecursion {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// buildIndex pre-compiles every configured record into ready-to-serve RRs.
func (h *DNSHandler) buildIndex() {
	for zone, records := range h.Config.Zones {
		h.zones = append(h.zones, strings.ToLower(zone))
		zoneDot := strings.ToLower(zone) + "."

		for _, rec := range records {
			var fqdn string
			if rec.Name == "@" {
				fqdn = zoneDot
			} else {
				fqdn = strings.ToLower(rec.Name) + "." + zoneDot
			}
			h.names[fqdn] = true

			rr, err := createRR(rec, fqdn)
			if err != nil {
				h.Logger.Errorf("Invalid DNS record %s %s in zone %s: %v", rec.Name, rec.Type, zone, err)
				continue
			}

			if rec.Type == "CNAME" {
				h.cnameIndex[fqdn] = append(h.cnameIndex[fqdn], rr)
				continue
			}

			qtype := dns.StringToType[rec.Type]
			if h.rrIndex[fqdn] == nil {
				h.rrIndex[fqdn] = make(map[uint16][]dns.RR)
			}
			h.rrIndex[fqdn][qtype] = append(h.rrIndex[fqdn][qtype], rr)
		}
	}

	sort.Slice(h.zones, func(i, j int) bool {
		return len(h.zones[i]) > len(h.zones[j])
	})
}

// matchZone returns the longest configured zone that contains name, matching
// only on label boundaries so that "evilexample.com." does not match the zone
// "example.com".
func (h *DNSHandler) matchZone(name string) string {
	for _, z := range h.zones {
		zoneDot := z + "."
		if name == zoneDot || strings.HasSuffix(name, "."+zoneDot) {
			return z
		}
	}
	return ""
}

// ServeDNS handles incoming DNS requests.
func (h *DNSHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	for _, q := range r.Question {
		name := strings.ToLower(q.Name)

		zone := h.matchZone(name)

		// If no zone found, try forwarding if configured and permitted.
		if zone == "" {
			if len(h.Config.UpstreamResolvers) > 0 {
				if !h.recursionAllowed(w.RemoteAddr()) {
					m := new(dns.Msg)
					m.SetRcode(r, dns.RcodeRefused)
					_ = w.WriteMsg(m)
					return
				}
				h.handleForwarding(w, r)
				return
			}
			msg.SetRcode(r, dns.RcodeNameError)
			continue
		}

		// Zone found, handle records
		answers, foundName := h.findRecords(name, q.Qtype)
		if len(answers) > 0 {
			msg.Answer = append(msg.Answer, answers...)
		} else if !foundName {
			// Name doesn't exist in zone -> NXDOMAIN
			msg.SetRcode(r, dns.RcodeNameError)
		} else {
			// Name exists but no record of requested type -> NODATA (Success with empty answer)
			// msg.Rcode is already Success by default
		}
	}

	// Set the TC (truncated) bit when the UDP response exceeds the client's
	// advertised buffer, so the client retries over TCP instead of timing out.
	if _, isUDP := w.RemoteAddr().(*net.UDPAddr); isUDP {
		maxSize := dns.MinMsgSize
		if opt := r.IsEdns0(); opt != nil {
			if sz := int(opt.UDPSize()); sz > maxSize {
				maxSize = sz
			}
		}
		msg.Truncate(maxSize)
	}

	if err := w.WriteMsg(msg); err != nil {
		h.Logger.Errorf("Failed to write DNS response: %v", err)
	}

	// Log after answering, off the response latency path.
	if clientIP, _, err := net.SplitHostPort(w.RemoteAddr().String()); err == nil {
		for _, q := range r.Question {
			h.Logger.Debugf("Query: %s %s from %s", q.Name, dns.TypeToString[q.Qtype], clientIP)
		}
	}
}

func (h *DNSHandler) findRecords(qname string, qtype uint16) (answers []dns.RR, foundName bool) {
	foundName = h.names[qname]
	if !foundName {
		return nil, false
	}

	if qtype != dns.TypeANY {
		if byType, ok := h.rrIndex[qname]; ok {
			answers = append(answers, byType[qtype]...)
		}
	}

	// A CNAME answers any query type on its name (RFC 1034); for TypeANY the
	// authoritative answer is the CNAME itself.
	answers = append(answers, h.cnameIndex[qname]...)

	return answers, foundName
}

func createRR(rec config.DNSRecord, qname string) (dns.RR, error) {
	header := dns.RR_Header{
		Name:   qname,
		Rrtype: dns.StringToType[rec.Type],
		Class:  dns.ClassINET,
		Ttl:    rec.TTL,
	}

	switch rec.Type {
	case "A":
		if ip := net.ParseIP(rec.Value); ip != nil {
			return &dns.A{Hdr: header, A: ip}, nil
		}
	case "AAAA":
		if ip := net.ParseIP(rec.Value); ip != nil {
			return &dns.AAAA{Hdr: header, AAAA: ip}, nil
		}
	case "CNAME":
		target := dns.Fqdn(rec.Value)
		return &dns.CNAME{Hdr: header, Target: target}, nil
	case "TXT":
		return &dns.TXT{Hdr: header, Txt: []string{rec.Value}}, nil
	case "NS":
		return &dns.NS{Hdr: header, Ns: dns.Fqdn(rec.Value)}, nil
	case "MX":
		return &dns.MX{Hdr: header, Preference: rec.Prio, Mx: dns.Fqdn(rec.Value)}, nil
	}
	return nil, fmt.Errorf("unsupported type")
}

func (h *DNSHandler) handleForwarding(w dns.ResponseWriter, r *dns.Msg) {
	// Simple forwarding with a shared, reused client.
	for _, upstream := range h.Config.UpstreamResolvers {
		target := upstream
		if !strings.Contains(target, ":") {
			target += ":53"
		}
		resp, _, err := h.client.Exchange(r, target)
		if err == nil {
			resp.Authoritative = false
			w.WriteMsg(resp)
			return
		}
		h.Logger.Errorf("Upstream query error to %s: %v", target, err)
	}
	// Fail if all upstreams fail
	m := new(dns.Msg)
	m.SetRcode(r, dns.RcodeServerFailure)
	w.WriteMsg(m)
}
