package dns

import (
	"fmt"
	"io"
	"sync"

	"github.com/miekg/dns"
	"github.com/mirkobrombin/goup/internal/config"
)

// dnsServerCloser adapts *dns.Server (which exposes Shutdown, not Close) to
// io.Closer so the lifecycle can drain it on shutdown.
type dnsServerCloser struct{ s *dns.Server }

func (c dnsServerCloser) Close() error { return c.s.Shutdown() }

// Start initiates the DNS server(s). Each created server is passed to register
// (when non-nil) so the caller can close it during graceful shutdown.
func Start(conf *config.DNSConfig, register func(io.Closer)) {
	handler, err := NewDNSHandler(conf)
	if err != nil {
		fmt.Printf("Error initializing DNS logger: %v\n", err)
		return
	}

	var wg sync.WaitGroup

	// UDP Server
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv := &dns.Server{
			Addr:      fmt.Sprintf(":%d", conf.Port),
			Net:       "udp",
			Handler:   handler,
			ReusePort: true,
		}
		if register != nil {
			register(dnsServerCloser{srv})
		}
		handler.Logger.Infof("Starting DNS UDP server on port %d", conf.Port)
		if err := srv.ListenAndServe(); err != nil {
			handler.Logger.Errorf("DNS UDP Error: %v", err)
		}
	}()

	// TCP Server
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv := &dns.Server{
			Addr:      fmt.Sprintf(":%d", conf.Port),
			Net:       "tcp",
			Handler:   handler,
			ReusePort: true,
		}
		if register != nil {
			register(dnsServerCloser{srv})
		}
		handler.Logger.Infof("Starting DNS TCP server on port %d", conf.Port)
		if err := srv.ListenAndServe(); err != nil {
			handler.Logger.Errorf("DNS TCP Error: %v", err)
		}
	}()

	// Keep alive
	wg.Wait()
}
