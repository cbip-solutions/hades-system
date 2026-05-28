//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package cache — ttl.go
//
// Per-source TTL rules engine for the research finding cache.
//
// The engine evaluates a slice of TTLRule in declaration order; the first
// matching rule wins ("first-match-wins"). Pattern matching uses simple
// host/path string checks (no regex) for predictability and speed.
//
// DefaultTTLRules is exported package-level so doctrine extension
// can wrap it with operator overrides from the [research.cache.ttl] TOML
// schema without modifying the cache package (open/closed principle).
//
// Rule order rationale:
// 1. rfc-permanent — RFC host + /rfc/ path; must precede docs-7d because
// rfc-editor.org pages often contain "/docs/" in sub-paths.
// 2. arxiv-permanent — arxiv.org host; no /docs/ overlap but placed early
// to keep permanent rules grouped.
// 3. github-state-1d — github.com + /issues/ | /pulls/ | /commits/ path;
// MUST precede docs-7d because github.com/*/docs/* would otherwise match
// docs-7d before reaching this rule.
// 4. docs-7d — readthedocs, docs.* host prefix, */docs/* path,
// developer.mozilla.org, pkg.go.dev.
// 5. default-1d — catch-all; always returns true.
package cache

import (
	"math"
	"net/url"
	"strings"
	"time"
)

// TTLPermanent is the sentinel duration for sources that never expire
// (RFC specifications, arXiv paper hashes). Modelled as math.MaxInt64
// nanoseconds so that any practical time.Sub call produces a value less
// than TTLPermanent, and IsExpired's `now.Sub(anchor) > ttl` comparison
// always returns false for permanent sources.
//
// Invariant (invariant-F8-001): TTLPermanent must equal math.MaxInt64
// nanoseconds. Changing this value would silently break IsExpired for
// all permanent sources. Do not substitute with a large finite constant.
const TTLPermanent = time.Duration(math.MaxInt64)

type TTLRule struct {
	Match func(parsed *url.URL, raw string) bool

	TTL time.Duration

	Name string
}

var DefaultTTLRules = []TTLRule{
	{
		Name: "rfc-permanent",
		TTL:  TTLPermanent,

		Match: func(u *url.URL, _ string) bool {
			if u == nil {
				return false
			}
			h := u.Host
			p := u.Path
			if strings.Contains(h, "rfc-editor.org") && strings.HasPrefix(p, "/rfc/") {
				return true
			}
			if strings.Contains(h, "datatracker.ietf.org") && strings.Contains(p, "/rfc") {
				return true
			}
			return false
		},
	},
	{
		Name: "arxiv-permanent",
		TTL:  TTLPermanent,

		Match: func(u *url.URL, _ string) bool {
			if u == nil {
				return false
			}
			return strings.Contains(u.Host, "arxiv.org")
		},
	},
	{
		Name: "github-state-1d",
		TTL:  24 * time.Hour,

		Match: func(u *url.URL, _ string) bool {
			if u == nil {
				return false
			}
			if !strings.HasSuffix(u.Host, "github.com") {
				return false
			}
			p := u.Path
			return strings.Contains(p, "/issues/") ||
				strings.Contains(p, "/pulls/") ||
				strings.Contains(p, "/commits/")
		},
	},
	{
		Name: "docs-7d",
		TTL:  7 * 24 * time.Hour,

		Match: func(u *url.URL, _ string) bool {
			if u == nil {
				return false
			}
			h := u.Host
			p := u.Path
			switch {
			case strings.Contains(h, "readthedocs.io"):
				return true
			case strings.HasPrefix(h, "docs."):
				return true
			case strings.Contains(p, "/docs/"):
				return true
			case strings.Contains(h, "developer.mozilla.org"):
				return true
			case strings.Contains(h, "pkg.go.dev"):
				return true
			default:
				return false
			}
		},
	},
	{
		Name: "default-1d",
		TTL:  24 * time.Hour,

		Match: func(_ *url.URL, _ string) bool { return true },
	},
}

// LookupTTL returns the effective TTL for sourceURL by evaluating
// DefaultTTLRules in declaration order. The first matching rule wins.
//
// Pre sourceURL may be any string (including empty or malformed URLs).
// Post returns a positive duration; never returns 0 or a negative value.
// If sourceURL cannot be parsed, matching falls back to rules that do not
// require a parsed URL (ultimately the catch-all default-1d).
//
// # Example
//
// ttl := LookupTTL("https://arxiv.org/abs/2509.17360")
// // ttl == TTLPermanent
func LookupTTL(sourceURL string) time.Duration {
	return LookupTTLRule(sourceURL).TTL
}

func LookupTTLRule(sourceURL string) TTLRule {
	parsed, _ := url.Parse(sourceURL)
	for _, rule := range DefaultTTLRules {
		if rule.Match(parsed, sourceURL) {
			return rule
		}
	}

	return DefaultTTLRules[len(DefaultTTLRules)-1]
}

func IsExpired(finding Finding, now time.Time) bool {

	lookupURL := finding.SourceURLCanonical
	if lookupURL == "" {
		lookupURL = finding.URL
	}

	ttl := LookupTTL(lookupURL)
	if ttl == TTLPermanent {

		return false
	}

	anchor := finding.RetrievalTimestamp
	if finding.LastValidatedAt != nil {
		anchor = *finding.LastValidatedAt
	}

	return now.Sub(anchor) > ttl
}
