//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package cache

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type SourceTTLConfig struct {
	Sources map[string]time.Duration
}

type sourceTTLConfigRaw struct {
	Sources map[string]string `toml:"sources"`
}

func LoadSourceTTLConfig(path string) (*SourceTTLConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {

			return &SourceTTLConfig{Sources: make(map[string]time.Duration)}, nil
		}
		return nil, fmt.Errorf("research_cache: read source-ttls %q: %w", path, err)
	}

	var raw sourceTTLConfigRaw
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("research_cache: parse source-ttls %q: %w", path, err)
	}

	cfg := &SourceTTLConfig{
		Sources: make(map[string]time.Duration, len(raw.Sources)),
	}
	for host, ttlStr := range raw.Sources {
		ttl, perr := parseTTLString(ttlStr)
		if perr != nil {
			return nil, fmt.Errorf("research_cache: source-ttls %q host %q: %w", path, host, perr)
		}
		cfg.Sources[host] = ttl
	}
	return cfg, nil
}

func (c *SourceTTLConfig) LookupFn() func(rawURL string) time.Duration {
	return func(rawURL string) time.Duration {
		if u, err := url.Parse(rawURL); err == nil && u != nil && u.Host != "" {
			if ttl, ok := c.Sources[u.Host]; ok {
				return ttl
			}
		}
		return LookupTTL(rawURL)
	}
}

func parseTTLString(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty TTL string")
	}

	if s == "permanent" {
		return TTLPermanent, nil
	}

	if strings.HasSuffix(s, "d") {
		nStr := strings.TrimSuffix(s, "d")
		n, perr := strconv.Atoi(nStr)
		if perr != nil {
			return 0, fmt.Errorf("invalid day count in TTL %q: %w", s, perr)
		}
		if n <= 0 {
			return 0, fmt.Errorf("day count must be a positive integer: %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}

	d, derr := time.ParseDuration(s)
	if derr != nil {
		return 0, fmt.Errorf("unsupported TTL format %q: %w", s, derr)
	}
	if d <= 0 {
		return 0, fmt.Errorf("TTL must be a positive duration: %q", s)
	}
	return d, nil
}
