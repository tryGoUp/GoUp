//go:build !web_only
// +build !web_only

package server

import (
	"sync"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/dns"
)

func launchDNS(wg *sync.WaitGroup) {
	config.GlobalConfMu.RLock()
	conf := config.GlobalConf
	config.GlobalConfMu.RUnlock()

	if conf != nil && conf.DNS != nil && conf.DNS.Enable {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dns.Start(conf.DNS, registerCloser)
		}()
	}
}
