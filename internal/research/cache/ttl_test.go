//go:build cgo
// +build cgo

package cache

import (
	"net/url"
	"testing"
	"time"
)

func TestLookupTTLDocsSites(t *testing.T) {
	cases := []string{
		"https://docs.python.org/3/library/json.html",
		"https://developer.mozilla.org/en-US/docs/Web/API",
		"https://example.readthedocs.io/page",
		"https://github.com/owner/repo/blob/main/docs/index.md",
		"https://pkg.go.dev/encoding/json",
	}
	for _, url := range cases {
		got := LookupTTL(url)
		want := 7 * 24 * time.Hour
		if got != want {
			t.Errorf("LookupTTL(%q) = %v, want %v (docs)", url, got, want)
		}
	}
}

func TestLookupTTLRFCPermanent(t *testing.T) {
	cases := []string{
		"https://www.rfc-editor.org/rfc/rfc9162.html",
		"https://datatracker.ietf.org/doc/rfc9162/",
	}
	for _, url := range cases {
		if got := LookupTTL(url); got != TTLPermanent {
			t.Errorf("LookupTTL(%q) = %v, want TTLPermanent", url, got)
		}
	}
}

func TestLookupTTLGitHubState(t *testing.T) {
	cases := []string{
		"https://github.com/owner/repo/issues/42",
		"https://github.com/owner/repo/pulls/100",
		"https://github.com/owner/repo/commits/abc123",
	}
	for _, url := range cases {
		got := LookupTTL(url)
		want := 24 * time.Hour
		if got != want {
			t.Errorf("LookupTTL(%q) = %v, want 24h (github state)", url, got)
		}
	}
}

func TestLookupTTLArxivPermanent(t *testing.T) {
	cases := []string{
		"https://arxiv.org/abs/2509.17360",
		"https://arxiv.org/pdf/2509.17360.pdf",
	}
	for _, url := range cases {
		if got := LookupTTL(url); got != TTLPermanent {
			t.Errorf("LookupTTL(%q) = %v, want TTLPermanent", url, got)
		}
	}
}

func TestLookupTTLDefaultFallback(t *testing.T) {
	got := LookupTTL("https://random.example.com/page")
	want := 24 * time.Hour
	if got != want {
		t.Errorf("default = %v, want 24h", got)
	}
}

func TestIsExpired(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	finding := Finding{
		SourceURLCanonical: "https://github.com/owner/repo/issues/1",
		RetrievalTimestamp: now.Add(-25 * time.Hour),
	}
	if !IsExpired(finding, now) {
		t.Errorf("expected expired (25h old, 24h TTL)")
	}

	finding.RetrievalTimestamp = now.Add(-12 * time.Hour)
	if IsExpired(finding, now) {
		t.Errorf("expected not expired (12h old, 24h TTL)")
	}
}

func TestIsExpiredUsesLastValidated(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	last := now.Add(-2 * time.Hour)
	finding := Finding{
		SourceURLCanonical: "https://github.com/owner/repo/issues/1",
		RetrievalTimestamp: now.Add(-25 * time.Hour),
		LastValidatedAt:    &last,
	}
	if IsExpired(finding, now) {
		t.Errorf("expected not expired: LastValidatedAt within TTL should override RetrievalTimestamp")
	}
}

func TestIsExpiredPermanentNeverExpires(t *testing.T) {
	now := time.Date(2099, 5, 7, 12, 0, 0, 0, time.UTC)
	finding := Finding{
		SourceURLCanonical: "https://www.rfc-editor.org/rfc/rfc9162.html",
		RetrievalTimestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if IsExpired(finding, now) {
		t.Errorf("permanent TTL never expires")
	}
}

func TestDefaultTTLRulesOrderPreserved(t *testing.T) {

	if len(DefaultTTLRules) < 5 {
		t.Errorf("DefaultTTLRules length = %d, want >= 5 (docs + RFC + github + arxiv + default)",
			len(DefaultTTLRules))
	}
}

func TestLookupTTLRuleNames(t *testing.T) {
	cases := []struct {
		url      string
		wantName string
	}{
		{"https://www.rfc-editor.org/rfc/rfc9162.html", "rfc-permanent"},
		{"https://datatracker.ietf.org/doc/rfc9162/", "rfc-permanent"},
		{"https://arxiv.org/abs/2509.17360", "arxiv-permanent"},
		{"https://arxiv.org/pdf/2509.17360.pdf", "arxiv-permanent"},
		{"https://github.com/owner/repo/issues/42", "github-state-1d"},
		{"https://github.com/owner/repo/pulls/100", "github-state-1d"},
		{"https://github.com/owner/repo/commits/abc123", "github-state-1d"},
		{"https://docs.python.org/3/library/json.html", "docs-7d"},
		{"https://example.readthedocs.io/page", "docs-7d"},
		{"https://developer.mozilla.org/en-US/docs/Web/API", "docs-7d"},
		{"https://pkg.go.dev/encoding/json", "docs-7d"},
		{"https://github.com/owner/repo/blob/main/docs/index.md", "docs-7d"},
		{"https://random.example.com/page", "default-1d"},
	}
	for _, tc := range cases {
		got := LookupTTLRule(tc.url)
		if got.Name != tc.wantName {
			t.Errorf("LookupTTLRule(%q).Name = %q, want %q", tc.url, got.Name, tc.wantName)
		}
		if got.TTL != LookupTTL(tc.url) {
			t.Errorf("LookupTTLRule(%q).TTL = %v, want %v (must match LookupTTL)", tc.url, got.TTL, LookupTTL(tc.url))
		}
	}
}

func TestIsExpiredFallsBackToURL(t *testing.T) {

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	finding := Finding{

		URL:                "https://github.com/owner/repo/issues/5",
		RetrievalTimestamp: now.Add(-25 * time.Hour),
	}
	if !IsExpired(finding, now) {
		t.Errorf("expected expired when SourceURLCanonical empty and URL used for lookup")
	}
}

func TestLookupTTLRuleEmptyRulesReturnsLast(t *testing.T) {

	orig := DefaultTTLRules

	neverMatch := TTLRule{
		Name:  "never",
		TTL:   1 * time.Second,
		Match: func(_ *url.URL, _ string) bool { return false },
	}
	DefaultTTLRules = []TTLRule{neverMatch, neverMatch}
	defer func() { DefaultTTLRules = orig }()

	got := LookupTTLRule("https://example.com/anything")

	if got.Name != "never" {
		t.Errorf("expected last-rule fallback, got %q", got.Name)
	}
}
