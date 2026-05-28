//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package sources implements concrete fetchers for the ecosystem
// ingestion pipeline.
//
// PkgGoDevSource implements the ecosystem.Source interface (master plan
// §3.3) for the Go ecosystem covering stdlib + the top-5000 packages by
// import count (per design contract=D corpus matrix).
//
// All HTTP egress routes via a narrow FetchClient interface that wraps
// cache.Revalidator.Fetch (invariant + invariant — single egress
// point for the research data plane; no direct net/http imports in this
// package). The narrow-interface pattern mirrors HADES design B-6 and B-2
// chunker contextual-prefix wiring: production wires *cache.Revalidator;
// tests wire a stub that pre-populates url→body and url→err maps.
//
// Per-source TTL = 7d (registered in A-10 source-ttls.toml and
// honored automatically by the Revalidator cache layer; no extra wiring
// is required here).
//
// Boundary (invariant): this package MAY import internal/research/cache
// + internal/research/ecosystem (parent) + golang.org/x/net/html (DOM
// parser, read-only stdlib-adjacent). It MUST NOT import internal/store
// or internal/providers.
package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type FetchClient interface {
	Fetch(ctx context.Context, url string, opts cache.FetchOptions) (*cache.FetchResult, error)
}

type PkgGoDevOptions struct {
	// Revalidator is the FetchClient used for every HTTP fetch. MUST be
	// non-nil at call time; the constructor does NOT require it (so
	// Ecosystem/Kind identity tests can run without wiring HTTP).
	Revalidator FetchClient

	BaseURL string

	IndexBaseURL string

	BlogBaseURL string

	MaxPackages int

	PerPage int

	HTTPTimeout time.Duration
}

type PkgGoDevSource struct {
	opts PkgGoDevOptions
}

var _ ecosystem.Source = (*PkgGoDevSource)(nil)

func NewPkgGoDevSource(opts PkgGoDevOptions) *PkgGoDevSource {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://pkg.go.dev"
	}
	if opts.IndexBaseURL == "" {
		opts.IndexBaseURL = "https://api.deps.dev"
	}
	if opts.BlogBaseURL == "" {
		opts.BlogBaseURL = "https://go.dev/blog"
	}
	if opts.MaxPackages == 0 {
		opts.MaxPackages = 5000
	}
	if opts.PerPage == 0 {
		opts.PerPage = 100
	}
	if opts.HTTPTimeout == 0 {
		opts.HTTPTimeout = 30 * time.Second
	}
	return &PkgGoDevSource{opts: opts}
}

func (s *PkgGoDevSource) Ecosystem() ecosystem.Ecosystem { return ecosystem.EcoGo }

func (s *PkgGoDevSource) Kind() ecosystem.SourceType { return ecosystem.SrcPackageDoc }

func (s *PkgGoDevSource) FetchManifest(ctx context.Context) (*ecosystem.Manifest, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("pkgdev: FetchManifest requires non-nil Revalidator")
	}

	pages := (s.opts.MaxPackages + s.opts.PerPage - 1) / s.opts.PerPage
	if pages < 1 {
		pages = 1
	}
	all := make([]ecosystem.ManifestPackage, 0, s.opts.MaxPackages)
	for page := 1; page <= pages; page++ {
		url := fmt.Sprintf("%s/v3alpha/systems/GO/dependents?per_page=%d&page=%d",
			s.opts.IndexBaseURL, s.opts.PerPage, page)
		ctxPage, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err := s.opts.Revalidator.Fetch(ctxPage, url, cache.FetchOptions{})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("pkgdev: fetch deps.dev page %d: %w", page, err)
		}
		pkgs, err := parseDepsDevPage(fr.Body)
		if err != nil {
			return nil, fmt.Errorf("pkgdev: parse deps.dev page %d: %w", page, err)
		}
		all = append(all, pkgs...)

		if len(pkgs) < s.opts.PerPage {
			break
		}

		if len(all) >= s.opts.MaxPackages {
			break
		}
	}
	if len(all) > s.opts.MaxPackages {
		all = all[:s.opts.MaxPackages]
	}
	return &ecosystem.Manifest{Packages: all}, nil
}

type depsDevResponse struct {
	Dependents []struct {
		VersionKey struct {
			System  string `json:"system"`
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"versionKey"`
		DependentCount       int `json:"dependentCount"`
		DirectDependentCount int `json:"directDependentCount"`
	} `json:"dependents"`
}

func parseDepsDevPage(body []byte) ([]ecosystem.ManifestPackage, error) {
	var resp depsDevResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]ecosystem.ManifestPackage, 0, len(resp.Dependents))
	for _, d := range resp.Dependents {
		out = append(out, ecosystem.ManifestPackage{
			Name:                d.VersionKey.Name,
			Versions:            []string{d.VersionKey.Version},
			LatestStableVersion: d.VersionKey.Version,
			UpstreamURL:         "https://pkg.go.dev/" + d.VersionKey.Name,
			LastUpdated:         now,
		})
	}
	return out, nil
}

func (s *PkgGoDevSource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("pkgdev: FetchPackageDoc requires non-nil Revalidator")
	}
	url := s.opts.BaseURL + "/" + pkg.CanonicalNamespace
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, url, cache.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("pkgdev: fetch %s: %w", url, err)
	}
	sections, err := parsePkgGoDevHTML(fr.Body, pkg.CanonicalNamespace)
	if err != nil {
		return nil, fmt.Errorf("pkgdev: parse %s HTML: %w", url, err)
	}
	return &ecosystem.PackageDoc{
		Package:   pkg,
		Version:   pkg.LatestStableVersion,
		Sections:  sections,
		RawBody:   string(fr.Body),
		SourceURL: url,
	}, nil
}

func parsePkgGoDevHTML(body []byte, canonicalNS string) ([]ecosystem.DocSection, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var sections []ecosystem.DocSection
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "h3" {
			return
		}
		class := htmlAttr(n, "class")
		id := htmlAttr(n, "id")
		if id == "" {
			return
		}
		var kind ecosystem.ChunkKind
		var astNode string
		switch {
		case strings.Contains(class, "Documentation-functionHeader"):
			kind = ecosystem.KindFunction
			astNode = "function_declaration"
		case strings.Contains(class, "Documentation-typeHeader"):
			kind = ecosystem.KindType
			astNode = "type_declaration"
		default:
			return
		}
		signature, body := extractFollowingSiblings(n)
		sections = append(sections, ecosystem.DocSection{
			Kind:        kind,
			SymbolPath:  canonicalNS + "." + id,
			Signature:   signature,
			Heading:     id,
			Body:        strings.TrimSpace(signature + "\n" + body),
			SourceURL:   "https://pkg.go.dev/" + canonicalNS + "#" + id,
			ASTNodeType: astNode,
		})
	})
	return sections, nil
}

func htmlAttr(n *html.Node, attr string) string {
	for _, a := range n.Attr {
		if a.Key == attr {
			return a.Val
		}
	}
	return ""
}

func walkHTML(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTML(c, fn)
	}
}

func extractFollowingSiblings(n *html.Node) (signature, body string) {
	var bodyParts []string
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if s.Type == html.ElementNode && (s.Data == "h2" || s.Data == "h3") {
			break
		}
		if s.Type != html.ElementNode {
			continue
		}
		switch s.Data {
		case "pre":
			text := nodeText(s)
			if signature == "" {
				signature = text
			} else {
				bodyParts = append(bodyParts, text)
			}
		case "p", "ul":
			bodyParts = append(bodyParts, nodeText(s))
		}
	}
	body = strings.Join(bodyParts, "\n")
	return signature, body
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	walkHTML(n, func(nn *html.Node) {
		if nn.Type == html.TextNode {
			sb.WriteString(nn.Data)
		}
	})
	return strings.TrimSpace(sb.String())
}

func (s *PkgGoDevSource) FetchChangelog(ctx context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("pkgdev: FetchChangelog requires non-nil Revalidator")
	}
	url := fmt.Sprintf("%s/go%s", s.opts.BlogBaseURL, version)
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, url, cache.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("pkgdev: fetch %s: %w", url, err)
	}
	entries, err := parseGoReleaseNotesHTML(fr.Body)
	if err != nil {
		return nil, fmt.Errorf("pkgdev: parse %s HTML: %w", url, err)
	}
	return &ecosystem.Changelog{
		Package:        pkg,
		VersionTo:      version,
		FormatDetected: "go-release-notes",
		Entries:        entries,
		RawText:        string(fr.Body),
		SourceURL:      url,
	}, nil
}

func parseGoReleaseNotesHTML(body []byte) ([]ecosystem.ChangelogEntry, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var entries []ecosystem.ChangelogEntry
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data != "h2" && n.Data != "h3" {
			return
		}
		heading := nodeText(n)
		if heading == "" {
			return
		}
		_, summary := extractFollowingSiblings(n)
		entries = append(entries, ecosystem.ChangelogEntry{
			ChangeType: classifyChangeType(heading, summary),
			SymbolPath: heading,
			Summary:    summary,
		})
	})
	return entries, nil
}

func classifyChangeType(heading, summary string) ecosystem.ChangeType {
	lower := strings.ToLower(heading + " " + summary)
	switch {
	case strings.Contains(lower, "deprecated"):
		return ecosystem.ChangeDeprecated
	case strings.Contains(lower, "removed"):
		return ecosystem.ChangeRemoved
	case strings.Contains(lower, "moved") || strings.Contains(lower, "renamed"):
		return ecosystem.ChangeMoved
	case strings.Contains(lower, "new") || strings.Contains(lower, "added"):
		return ecosystem.ChangeAdded
	}
	return ecosystem.ChangeChanged
}
