package dns

import (
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"
	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
)

// DNSHandler implements the dns.Handler interface.
type DNSHandler struct {
	Config *config.DNSConfig
	Logger *logger.Logger
}

// NewDNSHandler creates a new DNS handler.
func NewDNSHandler(conf *config.DNSConfig) (*DNSHandler, error) {
	l, err := logger.NewSystemLogger("DNS")
	if err != nil {
		return nil, err
	}
	return &DNSHandler{
		Config: conf,
		Logger: l,
	}, nil
}

// ServeDNS handles incoming DNS requests.
func (h *DNSHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	// Log the query
	clientIP, _, _ := net.SplitHostPort(w.RemoteAddr().String())
	for _, q := range r.Question {
		h.Logger.Infof("Query: %s %s from %s", q.Name, dns.TypeToString[q.Qtype], clientIP)
	}

	for _, q := range r.Question {
		name := strings.ToLower(q.Name)

		// Look for zone match
		var zone string
		for z := range h.Config.Zones {
			if strings.HasSuffix(name, z+".") {
				zone = z
				break
			}
		}

		// If no zone found, try forwarding if configured
		if zone == "" {
			if len(h.Config.UpstreamResolvers) > 0 {
				h.handleForwarding(w, r)
				return
			}
			msg.SetRcode(r, dns.RcodeNameError)
			continue
		}

		// Zone found, handle records
		answers, foundName := h.findRecords(zone, name, q.Qtype)
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

	w.WriteMsg(msg)
}

func (h *DNSHandler) findRecords(zone, qname string, qtype uint16) (answers []dns.RR, foundName bool) {
	configRecords, ok := h.Config.Zones[zone]
	if !ok {
		return nil, false
	}

	zoneDot := zone + "."
	var relative string
	if qname == zoneDot {
		relative = "@"
	} else if strings.HasSuffix(qname, "."+zoneDot) {
		relative = strings.TrimSuffix(qname, "."+zoneDot)
	} else {
		return nil, false
	}

	for _, rec := range configRecords {
		if rec.Name == relative {
			foundName = true

			// CNAME handling (RFC 1034)
			if rec.Type == "CNAME" {
				if qtype == dns.TypeCNAME || qtype == dns.TypeANY {
					rr, err := h.createRR(rec, qname)
					if err == nil {
						answers = append(answers, rr)
					}
				} else {
					// We found a CNAME but requested another type.
					// Authoritative server should return the CNAME.
					rr, err := h.createRR(rec, qname)
					if err == nil {
						answers = append(answers, rr)
					}
				}
				continue
			}

			if rec.Type == dns.TypeToString[qtype] {
				rr, err := h.createRR(rec, qname)
				if err == nil {
					answers = append(answers, rr)
				}
			}
		}
	}
	return answers, foundName
}

func (h *DNSHandler) createRR(rec config.DNSRecord, qname string) (dns.RR, error) {
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
	// Simple forwarding
	for _, upstream := range h.Config.UpstreamResolvers {
		target := upstream
		if !strings.Contains(target, ":") {
			target += ":53"
		}
		resp, _, err := new(dns.Client).Exchange(r, target)
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
