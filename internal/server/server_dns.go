//go:build !web_only
// +build !web_only

package server

import (
	"sync"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/dns"
)

func launchDNS(wg *sync.WaitGroup) {
	if config.GlobalConf != nil && config.GlobalConf.DNS != nil && config.GlobalConf.DNS.Enable {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dns.Start(config.GlobalConf.DNS)
		}()
	}
}
