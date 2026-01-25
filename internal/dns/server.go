package dns

import (
	"fmt"
	"sync"

	"github.com/miekg/dns"
	"github.com/mirkobrombin/goup/internal/config"
)

// Start initiates the DNS server(s).
func Start(conf *config.DNSConfig) {
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
		handler.Logger.Infof("Starting DNS TCP server on port %d", conf.Port)
		if err := srv.ListenAndServe(); err != nil {
			handler.Logger.Errorf("DNS TCP Error: %v", err)
		}
	}()

	// Keep alive
	wg.Wait()
}
