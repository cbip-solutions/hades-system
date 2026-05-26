//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// internal/research/ecosystem/sources/arxiv.go
//
//
// arXiv (https://arxiv.org) exposes a public Atom XML search API at
// https://export.arxiv.org/api/query that returns paper metadata for a
// given category-OR query. Phase B's ArxivSource wraps that API behind the
// ecosystem.Source interface (master plan §3.3) so the Phase B ingester
// can pull arXiv CS / PL / ML papers into ecosystem.db alongside the
// per-language package documentation produced by pkgdev/pypi/npm/cratesio.
//
// Instance topology (per spec §2.1 + Stage 2 amendment 2026-05-15):
// arXiv is 1-instance-per-ecosystem (4 instances total: one each in
// Go/Python/TypeScript/Rust daemon Source maps), in contrast to the
// pkgdev/pypi/npm/cratesio sources which are 1-instance-total. Each
// per-ecosystem instance filters manifest pulls by ecosystem-relevant
// arXiv categories (Go → cs.PL+cs.SE; Python → cs.PL+cs.ML+stat.ML;
// TypeScript → cs.PL+cs.HC; Rust → cs.PL+cs.OS+cs.DC) so each
// ecosystem.db ingests its relevant subset. Daemon-init wires 4× ArxivSource
// instances under Source map[Ecosystem]map[SourceType]Source.
//
// FetchManifest queries export.arxiv.org/api/query with the configured
// category-OR query, parses the Atom XML response via encoding/xml, and
// returns the top-2000 papers ranked by submission date (newest first).
// Each entry becomes a ManifestPackage whose Name is the bare arXiv ID
// (e.g., "2506.15655", version suffix stripped) and whose UpstreamURL is
// the canonical arxiv.org/abs page.
//
// FetchPackageDoc fetches the arxiv.org/abs HTML page for the paper's
// title + abstract + authors (always) and optionally the arxiv.org/pdf
// body for full-text chunking (IncludePDF opt-in). The PDF is parsed via
// github.com/ledongthuc/pdf — best-effort: if the fetch errors, the PDF
// body is empty, or the parser fails, the implementation skips the PDF
// section silently (no failure propagation) and returns the
// abstract-only PackageDoc. Per-paper TTL = permanent (paper-hash stable
// per ADR-0067; arxiv papers are immutable). FetchChangelog returns
// Changelog{FormatDetected: "not-available"} for every (package, version)
// pair — arxiv papers have no changelog surface (re-submitted v2/v3
// versions are addressed by the daemon's per-version PackageRef separately).
//
// All HTTP egress routes via the narrow FetchClient interface declared in
// pkgdev.go (inv-zen-152 + inv-zen-191 — single egress point for the
// research data plane; no direct net/http imports in this package).
//
// Boundary (inv-zen-031): this file MAY import
// internal/research/cache + internal/research/ecosystem (parent) +
// encoding/xml + github.com/ledongthuc/pdf (stdlib + read-only PDF
// extraction). It MUST NOT import internal/store, internal/providers,
// internal/daemon, or any net/http symbols.

package sources

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type ArxivOptions struct {
	// Revalidator is the FetchClient used for every HTTP fetch. MUST be
	// non-nil at call time; the constructor does NOT require it (so
	// Ecosystem/Kind identity tests can run without wiring HTTP).
	Revalidator FetchClient

	Ecosystem ecosystem.Ecosystem

	BaseURL string

	AbsURL string

	PDFURL string

	Categories []string

	MaxResults int

	HTTPTimeout time.Duration

	IncludePDF bool
}

type ArxivSource struct {
	opts ArxivOptions
}

var _ ecosystem.Source = (*ArxivSource)(nil)

func NewArxivSource(opts ArxivOptions) *ArxivSource {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://export.arxiv.org/api"
	}
	if opts.AbsURL == "" {
		opts.AbsURL = "https://arxiv.org/abs"
	}
	if opts.PDFURL == "" {
		opts.PDFURL = "https://arxiv.org/pdf"
	}
	if opts.MaxResults == 0 {
		opts.MaxResults = 2000
	}
	if opts.HTTPTimeout == 0 {
		opts.HTTPTimeout = 60 * time.Second
	}
	if len(opts.Categories) == 0 {
		opts.Categories = defaultCategoriesFor(opts.Ecosystem)
	}
	return &ArxivSource{opts: opts}
}

func defaultCategoriesFor(eco ecosystem.Ecosystem) []string {
	switch eco {
	case ecosystem.EcoGo:
		return []string{"cs.PL", "cs.SE"}
	case ecosystem.EcoPython:
		return []string{"cs.PL", "cs.ML", "stat.ML"}
	case ecosystem.EcoTypeScript:
		return []string{"cs.PL", "cs.HC"}
	case ecosystem.EcoRust:
		return []string{"cs.PL", "cs.OS", "cs.DC"}
	}
	return []string{"cs.PL"}
}

func (s *ArxivSource) Ecosystem() ecosystem.Ecosystem { return s.opts.Ecosystem }

func (s *ArxivSource) Kind() ecosystem.SourceType { return ecosystem.SrcArXiv }

func (s *ArxivSource) FetchManifest(ctx context.Context) (*ecosystem.Manifest, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("arxiv: nil Revalidator")
	}
	q := buildArxivQuery(s.opts.Categories)
	uri := fmt.Sprintf(
		"%s/query?search_query=%s&start=0&max_results=%d&sortBy=submittedDate&sortOrder=descending",
		s.opts.BaseURL, q, s.opts.MaxResults,
	)
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("arxiv: fetch query: %w", err)
	}
	entries, err := parseArxivAtom(fr.Body)
	if err != nil {
		return nil, fmt.Errorf("arxiv: parse Atom: %w", err)
	}
	out := make([]ecosystem.ManifestPackage, 0, len(entries))
	for _, e := range entries {
		id := extractArxivID(e.ID)
		out = append(out, ecosystem.ManifestPackage{
			Name:        id,
			UpstreamURL: s.opts.AbsURL + "/" + id,
			LastUpdated: e.Updated,
		})
	}
	return &ecosystem.Manifest{Packages: out}, nil
}

func buildArxivQuery(cats []string) string {
	terms := make([]string, len(cats))
	for i, c := range cats {
		terms[i] = "cat:" + c
	}
	return strings.Join(terms, "+OR+")
}

type arxivAtomEntry struct {
	ID      string    `xml:"id"`
	Updated time.Time `xml:"updated"`
	Title   string    `xml:"title"`
	Summary string    `xml:"summary"`
	Authors []struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

type arxivAtomFeed struct {
	XMLName xml.Name         `xml:"feed"`
	Entries []arxivAtomEntry `xml:"entry"`
}

func parseArxivAtom(body []byte) ([]arxivAtomEntry, error) {
	var feed arxivAtomFeed
	dec := xml.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&feed); err != nil {
		return nil, err
	}
	return feed.Entries, nil
}

func extractArxivID(rawID string) string {
	const httpPrefix = "http://arxiv.org/abs/"
	const httpsPrefix = "https://arxiv.org/abs/"
	id := strings.TrimPrefix(rawID, httpPrefix)
	id = strings.TrimPrefix(id, httpsPrefix)

	if idx := strings.LastIndex(id, "v"); idx > 0 {
		afterV := id[idx+1:]
		if isAllDigits(afterV) {
			id = id[:idx]
		}
	}
	return id
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (s *ArxivSource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("arxiv: nil Revalidator")
	}
	absURI := s.opts.AbsURL + "/" + pkg.Name
	ctxAbs, cancelAbs := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	frAbs, err := s.opts.Revalidator.Fetch(ctxAbs, absURI, cache.FetchOptions{})
	cancelAbs()
	if err != nil {
		return nil, fmt.Errorf("arxiv: fetch abs: %w", err)
	}
	sections := []ecosystem.DocSection{
		{
			Kind:        ecosystem.KindGuide,
			SymbolPath:  "arxiv:" + pkg.Name,
			Heading:     "Abstract",
			Body:        string(frAbs.Body),
			SourceURL:   absURI,
			ASTNodeType: "document",
		},
	}
	if s.opts.IncludePDF {
		pdfURI := s.opts.PDFURL + "/" + pkg.Name
		ctxPDF, cancelPDF := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		frPDF, errPDF := s.opts.Revalidator.Fetch(ctxPDF, pdfURI, cache.FetchOptions{})
		cancelPDF()
		if errPDF == nil && len(frPDF.Body) > 0 {
			text, errExtract := extractPDFText(frPDF.Body)
			if errExtract == nil && text != "" {
				sections = append(sections, ecosystem.DocSection{
					Kind:        ecosystem.KindGuide,
					SymbolPath:  "arxiv:" + pkg.Name,
					Heading:     "Full Paper",
					Body:        text,
					SourceURL:   pdfURI,
					ASTNodeType: "document",
				})
			}
		}
	}
	return &ecosystem.PackageDoc{
		Package:   pkg,
		Version:   "v1",
		Sections:  sections,
		RawBody:   string(frAbs.Body),
		SourceURL: absURI,
	}, nil
}

func extractPDFText(data []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	numPages := reader.NumPage()
	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page := reader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func (s *ArxivSource) FetchChangelog(ctx context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error) {
	return &ecosystem.Changelog{
		Package:        pkg,
		VersionTo:      version,
		FormatDetected: "not-available",
	}, nil
}
