//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// internal/research/ecosystem/sources/mdn.go
//
// ecosystem only. Implements ecosystem.Source (master plan §3.3).
//
// MDN (developer.mozilla.org) is the canonical web-platform documentation.
// MDNSource only fires for the typescript ecosystem since MDN
// covers JavaScript/TypeScript Web APIs (Array, Promise, fetch, DOM, etc.)
// — Go/Python/Rust have no MDN equivalent. The 1-instance-total topology
// (single MDNSource registered under Source[EcoTypeScript][SrcMDN])
// matches pkgdev/pypi/npm/cratesio (each is the sole authority for its
// ecosystem×source combination) and contrasts with arXiv's
// 1-instance-per-ecosystem topology (B-8).
//
// FetchManifest fetches https://developer.mozilla.org/sitemap.xml (~30k
// canonical URLs) and filters to the JS subtree (/Web/JavaScript/Reference/)
// + Web-API subtree (/Web/API/) for the TypeScript ecosystem — ~6,000
// docs total. CSS subtree (/Web/CSS/) and HTML-element subtree
// (/Web/HTML/) are opt-in via IncludeCSS / IncludeHTMLEl flags (default
// false; orthogonal to TS ecosystem semantics).
//
// FetchPackageDoc fetches the individual MDN page and walks <article>
// h1/h2 sections via golang.org/x/net/html to emit DocSection entries
// (Kind=KindGuide). When the page exposes no h1/h2 boundaries the
// implementation falls back to a single bulk section so the downstream
// chunker still receives non-empty input.
//
// FetchChangelog returns Changelog{FormatDetected: "not-available"} for
// every (package, version) pair — MDN has no per-API changelog; cross-doc
// changes happen via the MDN content git repo at
// github.com/mdn/content (out of scope for ; defer to operator
// manual reindex via `hades research mdn reindex` post-v0.14.0).
//
// All HTTP egress routes via the narrow FetchClient interface declared in
// pkgdev.go (invariant + invariant — single egress point for the
// research data plane; no direct net/http imports in this package).
//
// Per-source TTL = 30d (registered in A-10 source-ttls.toml; MDN
// updates slowly so the polling cadence is the lowest of any source).
//
// Boundary (invariant): this file MAY import
// internal/research/cache + internal/research/ecosystem (parent) +
// golang.org/x/net/html. It MUST NOT import internal/store, internal/
// providers, internal/daemon, or any net/http symbols.

package sources

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type MDNOptions struct {
	// Revalidator is the FetchClient used for every HTTP fetch. MUST be
	// non-nil at call time; the constructor does NOT require it (so
	// Ecosystem/Kind identity tests can run without wiring HTTP).
	Revalidator FetchClient

	BaseURL string

	SitemapPath string

	HTTPTimeout time.Duration

	IncludeCSS bool

	IncludeHTMLEl bool
}

type MDNSource struct {
	opts MDNOptions
}

var _ ecosystem.Source = (*MDNSource)(nil)

func NewMDNSource(opts MDNOptions) *MDNSource {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://developer.mozilla.org"
	}
	if opts.SitemapPath == "" {
		opts.SitemapPath = "/sitemap.xml"
	}
	if opts.HTTPTimeout == 0 {
		opts.HTTPTimeout = 30 * time.Second
	}
	return &MDNSource{opts: opts}
}

func (s *MDNSource) Ecosystem() ecosystem.Ecosystem { return ecosystem.EcoTypeScript }

func (s *MDNSource) Kind() ecosystem.SourceType { return ecosystem.SrcMDN }

func (s *MDNSource) FetchManifest(ctx context.Context) (*ecosystem.Manifest, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("mdn: nil Revalidator")
	}
	uri := s.opts.BaseURL + s.opts.SitemapPath
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("mdn: fetch sitemap: %w", err)
	}
	urls, err := parseMDNSitemap(fr.Body)
	if err != nil {
		return nil, fmt.Errorf("mdn: parse sitemap: %w", err)
	}
	out := make([]ecosystem.ManifestPackage, 0, len(urls))
	for _, u := range urls {
		if !s.includeURL(u.Loc) {
			continue
		}
		out = append(out, ecosystem.ManifestPackage{
			Name:        urlToCanonical(u.Loc),
			UpstreamURL: u.Loc,
			LastUpdated: u.LastMod,
		})
	}
	return &ecosystem.Manifest{Packages: out}, nil
}

func (s *MDNSource) includeURL(url string) bool {
	if strings.Contains(url, "/Web/JavaScript/Reference/") {
		return true
	}
	if strings.Contains(url, "/Web/API/") {
		return true
	}
	if s.opts.IncludeCSS && strings.Contains(url, "/Web/CSS/") {
		return true
	}
	if s.opts.IncludeHTMLEl && strings.Contains(url, "/Web/HTML/") {
		return true
	}
	return false
}

func urlToCanonical(u string) string {
	const prefix = "https://developer.mozilla.org/en-US/docs/"
	if !strings.HasPrefix(u, prefix) {
		return u
	}
	rest := strings.TrimPrefix(u, prefix)
	parts := strings.Split(rest, "/")
	return strings.Join(parts, ".")
}

type mdnSitemapURL struct {
	Loc     string    `xml:"loc"`
	LastMod time.Time `xml:"lastmod"`
}

type mdnSitemapURLSet struct {
	XMLName xml.Name        `xml:"urlset"`
	URLs    []mdnSitemapURL `xml:"url"`
}

func parseMDNSitemap(body []byte) ([]mdnSitemapURL, error) {
	var s mdnSitemapURLSet
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	return s.URLs, nil
}

func (s *MDNSource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("mdn: nil Revalidator")
	}
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, pkg.UpstreamURL, cache.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("mdn: fetch page: %w", err)
	}
	sections, err := parseMDNPage(fr.Body, pkg.CanonicalNamespace, pkg.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("mdn: parse page: %w", err)
	}
	return &ecosystem.PackageDoc{
		Package:   pkg,
		Version:   "latest",
		Sections:  sections,
		RawBody:   string(fr.Body),
		SourceURL: pkg.UpstreamURL,
	}, nil
}

func parseMDNPage(body []byte, canonical, url string) ([]ecosystem.DocSection, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var sections []ecosystem.DocSection
	var currentHeading string
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		switch n.Data {
		case "h1", "h2":
			currentHeading = nodeText(n)
		case "p":
			if currentHeading == "" {
				return
			}
			body := nodeText(n)
			if body == "" {
				return
			}
			sections = append(sections, ecosystem.DocSection{
				Kind:        ecosystem.KindGuide,
				SymbolPath:  canonical,
				Heading:     currentHeading,
				Body:        body,
				SourceURL:   url,
				ASTNodeType: "document",
			})
		}
	})
	if len(sections) == 0 {
		sections = append(sections, ecosystem.DocSection{
			Kind:        ecosystem.KindGuide,
			SymbolPath:  canonical,
			Heading:     canonical,
			Body:        nodeText(doc),
			SourceURL:   url,
			ASTNodeType: "document",
		})
	}
	return sections, nil
}

func (s *MDNSource) FetchChangelog(_ context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error) {
	return &ecosystem.Changelog{
		Package:        pkg,
		VersionTo:      version,
		FormatDetected: "not-available",
	}, nil
}
