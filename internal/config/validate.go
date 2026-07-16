package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"
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
func ValidateAll() (fileErrors map[string][]string, crossErrors []string, err error) {
	dir := GetConfigDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}

	fileErrors = make(map[string][]string)
	seenDomains := make(map[string]string)
	portSSL := make(map[int]*bool)

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
		if problems := conf.Validate(); len(problems) > 0 {
			fileErrors[name] = problems
		}

		if prev, ok := seenDomains[conf.Domain]; ok {
			crossErrors = append(crossErrors, fmt.Sprintf("duplicate domain %q in %s and %s", conf.Domain, prev, name))
		} else {
			seenDomains[conf.Domain] = name
		}

		ssl := conf.SSL.Enabled
		if prev, ok := portSSL[conf.Port]; ok {
			if *prev != ssl {
				crossErrors = append(crossErrors, fmt.Sprintf("port %d mixes SSL and non-SSL sites", conf.Port))
			}
		} else {
			portSSL[conf.Port] = &ssl
		}
	}

	return fileErrors, crossErrors, nil
}
