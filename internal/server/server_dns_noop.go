//go:build web_only
// +build web_only

package server

import "sync"

// launchDNS is a no-op when the binary is built with the web_only tag.
func launchDNS(wg *sync.WaitGroup) {}
