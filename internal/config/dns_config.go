package config

// DNSRecord represents a single DNS record entry.
type DNSRecord struct {
	Type  string `json:"type"`  // A, AAAA, CNAME, TXT, MX, NS
	Name  string `json:"name"`  // @ for apex, or subdomain
	Value string `json:"value"` // IP, target, or text
	TTL   uint32 `json:"ttl"`   // Time-to-live in seconds
	Prio  uint16 `json:"prio"`  // Priority (for MX records)
}

// DNSConfig defines configuration for the integrated DNS server.
type DNSConfig struct {
	Enable            bool                   `json:"enable"`
	Port              int                    `json:"port"`               // Default: 53
	UpstreamResolvers []string               `json:"upstream_resolvers"` // Optional forwarding
	Zones             map[string][]DNSRecord `json:"zones"`              // zone -> records
}

// DefaultDNSConfig returns the default DNS configuration.
func DefaultDNSConfig() *DNSConfig {
	return &DNSConfig{
		Enable:            false,
		Port:              53,
		UpstreamResolvers: []string{},
		Zones:             make(map[string][]DNSRecord),
	}
}
