package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/configuration"
)

// Validate performs semantic validation of a site configuration and returns a
// list of human-readable problems (empty = valid). It is shared by the CLI
// `validate` command and the API validation endpoint so both agree on what a
// valid site looks like.
func (c SiteConfig) Validate() []string {
	var errs []string

	if err := ValidateDomain(c.Domain); err != nil {
		errs = append(errs, "domain: "+err.Error())
	}
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Sprintf("port %d is out of range (1-65535)", c.Port))
	}

	if c.ProxyPass == "" && c.RootDirectory == "" && len(c.ProxyUpstreams) == 0 {
		errs = append(errs, "either proxy_pass, proxy_upstreams or root_directory must be set")
	}
	if c.ProxyPass != "" {
		if u, err := url.Parse(c.ProxyPass); err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, "proxy_pass must be an absolute URL (e.g. http://localhost:3000)")
		}
	}
	for _, up := range c.ProxyUpstreams {
		if u, err := url.Parse(up); err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, "proxy_upstreams contains an invalid URL: "+up)
		}
	}

	// Only a static site (no proxy) needs a readable root directory.
	if c.RootDirectory != "" && c.ProxyPass == "" && len(c.ProxyUpstreams) == 0 {
		exists, invalid := CheckPath(c.RootDirectory)
		switch {
		case invalid:
			errs = append(errs, "root_directory must be an absolute path without '..'")
		case !exists:
			errs = append(errs, "root_directory does not exist")
		}
	}

	if c.SSL.Enabled {
		if c.SSL.Certificate == "" || c.SSL.Key == "" {
			errs = append(errs, "ssl.enabled requires both certificate and key")
		} else {
			if exists, invalid := CheckPath(c.SSL.Certificate); invalid || !exists {
				errs = append(errs, "ssl certificate not found (must be an absolute path)")
			}
			if exists, invalid := CheckPath(c.SSL.Key); invalid || !exists {
				errs = append(errs, "ssl key not found (must be an absolute path)")
			}
		}
	}

	if c.FlushInterval != "" {
		if _, err := time.ParseDuration(c.FlushInterval); err != nil {
			errs = append(errs, "proxy_flush_interval is not a valid duration (e.g. \"100ms\")")
		}
	}

	return errs
}

// StrictParseSiteConfig parses a site config file rejecting unknown fields, so a
// typo like "prot" instead of "port" is reported instead of silently ignored.
func StrictParseSiteConfig(path string) (SiteConfig, error) {
	var conf SiteConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return conf, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&conf); err != nil {
		return conf, err
	}
	return conf, nil
}

// ValidateAll validates every site config file in the config directory and
// returns a per-file map of problems plus any cross-site conflicts (duplicate
// domains, or a port shared by sites that disagree on SSL).
//
// The two failure modes map onto Foundation's collection validation: per-file
// rules run as the item validator (keyed by file name, which is the entry
// name here) and cross-site conflicts as cross validators.
func ValidateAll() (fileErrors map[string][]string, crossErrors []string, err error) {
	dir := GetConfigDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}

	fileErrors = make(map[string][]string)
	confs := make(map[string]*SiteConfig)

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == globalConfName || len(name) < 6 || name[len(name)-5:] != ".json" {
			continue
		}
		full, jerr := SafeJoin(dir, name)
		if jerr != nil {
			fileErrors[name] = []string{jerr.Error()}
			continue
		}
		conf, perr := StrictParseSiteConfig(full)
		if perr != nil {
			fileErrors[name] = []string{"parse error: " + perr.Error()}
			continue
		}
		confs[name] = &conf
	}

	for _, p := range configuration.ValidateCollection(confs,
		func(name string, conf *SiteConfig) []string { return conf.Validate() },
		duplicateDomains, conflictingPortSSL,
	) {
		// Item problems come back prefixed with the entry name; split the
		// prefix back out to keep the per-file return shape.
		if n, rest, ok := strings.Cut(p, ": "); ok && confs[n] != nil {
			fileErrors[n] = append(fileErrors[n], rest)
		} else {
			crossErrors = append(crossErrors, p)
		}
	}

	return fileErrors, crossErrors, nil
}

// duplicateDomains reports sites that declare the same domain in two files.
func duplicateDomains(confs map[string]*SiteConfig) []string {
	seen := make(map[string]string)
	var out []string
	for _, name := range sortedKeys(confs) {
		d := confs[name].Domain
		if prev, ok := seen[d]; ok {
			out = append(out, fmt.Sprintf("duplicate domain %q in %s and %s", d, prev, name))
		} else {
			seen[d] = name
		}
	}
	return out
}

// conflictingPortSSL reports ports shared by sites that disagree on SSL.
func conflictingPortSSL(confs map[string]*SiteConfig) []string {
	portSSL := make(map[int]bool)
	var out []string
	for _, name := range sortedKeys(confs) {
		conf := confs[name]
		if prev, ok := portSSL[conf.Port]; ok {
			if prev != conf.SSL.Enabled {
				out = append(out, fmt.Sprintf("port %d mixes SSL and non-SSL sites", conf.Port))
			}
		} else {
			portSSL[conf.Port] = conf.SSL.Enabled
		}
	}
	return out
}

func sortedKeys(confs map[string]*SiteConfig) []string {
	names := make([]string, 0, len(confs))
	for name := range confs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
