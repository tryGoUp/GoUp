//go:build dns_only
// +build dns_only

package server

import (
	"sync"

	"github.com/mirkobrombin/goup/internal/config"
)

// launchWebComponents is a no-op when the binary is built with the dns_only tag.
func launchWebComponents(configs []config.SiteConfig, enableTUI bool, enableBench bool, wg *sync.WaitGroup) {
}
