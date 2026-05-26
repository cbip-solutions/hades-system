//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// internal/research/ecosystem/sources/cratesio.go
//
// Implements ecosystem.Source (master plan §3.3).
//
// FetchManifest queries the crates.io v1 endpoint
// (`https://crates.io/api/v1/crates?sort=recent-downloads&per_page=100&page=N`)
// which returns top crates ranked by recent-download count. Pagination is
// driven by page=1..M at PerPage (default 100) up to MaxPackages (default
// 1000). Rust's crate ecosystem is roughly an order of magnitude smaller
// than JS/Python so the default cap is 1000 (vs 5000 for npm); the
// MaxPackages override allows a wider sweep when an operator opts in.
//
// FetchPackageDoc fetches `https://crates.io/api/v1/crates/<crate>` and
// emits a PackageDoc with two DocSections: Description (KindModule, from
// the API's "crate.description" field, tagged with ASTNodeType
// "source_file" — the tree-sitter Rust grammar's root node) and README
// (KindGuide, from the API's "crate.readme" markdown body). The full
// crates.io JSON body is preserved in RawBody for downstream chunker
// re-parsing in Phase B chunker.go (which applies tree-sitter Rust grammar
// to per-symbol code blocks AND tree-sitter Markdown to README bodies).
//
// When the opt-in FetchDocsRs option is set, FetchPackageDoc additionally
// fetches `https://docs.rs/<crate>/<version>/<crate>/index.html` (the
// rustdoc-generated HTML landing page) and appends it as a third
// DocSection (KindGuide, ASTNodeType "document"). The docs.rs fetch is
// best-effort: any error OR empty body is silently skipped (docs.rs has
// stricter rate limits than crates.io, and per-version HTML can be
// missing for old crates). The authoritative source is always the
// crates.io JSON.
//
// FetchChangelog derives a GitHub repository URL from the crate metadata's
// `crate.repository` field (a free-form string; crates.io does not
// constrain it to a particular VCS host) and probes two known changelog
// locations in order:
//   1. master/CHANGELOG.md  (Keep-a-Changelog on master — older convention)
//   2. main/CHANGELOG.md    (Keep-a-Changelog on main — modern default)
// First hit wins. When no candidate succeeds OR when no repository URL is
// discoverable OR when the repository URL is non-GitHub, returns a non-nil
// Changelog with FormatDetected = "not-available" (NOT an error — many
// Rust crates ship without a CHANGELOG.md, relying on git log or
// release-tag annotations instead).
//
// FormatDetected is set to "keep-a-changelog" on hit (the body is parsed
// via the same parseKeepAChangelog helper used by pypi.go + npm.go, which
// extracts list-bullet entries under heading rows).
//
// All HTTP egress routes via the narrow FetchClient interface declared in
// pkgdev.go; no direct net/http imports (inv-zen-152 + inv-zen-191).
// Per-source TTL = 7d (registered in source-ttls.toml at A-10; crates
// publish less frequently than npm/pypi).
//
// Boundary (inv-zen-031): this file MAY import internal/research/cache
// + internal/research/ecosystem (parent). It MUST NOT import internal/store
// or internal/providers. Verified by `go test -run TestEcosystemBoundary`
// and Phase H vet analyzer no_web_in_ecosystem.
//
// Concurrency CratesIOSource is safe for concurrent use from multiple
// goroutines after construction (immutable opts; the underlying
// FetchClient is concurrency-safe per cache.Revalidator contract).

package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type CratesIOOptions struct {
	// Revalidator is the FetchClient used for every HTTP fetch. MUST be
	// non-nil when any Fetch* method is called; constructor does NOT
	// require it so Ecosystem/Kind identity tests can run without HTTP wiring.
	Revalidator FetchClient

	BaseURL string

	DocsRsURL string

	MaxPackages int

	PerPage int

	HTTPTimeout time.Duration

	FetchDocsRs bool
}

type CratesIOSource struct {
	opts CratesIOOptions
}

var _ ecosystem.Source = (*CratesIOSource)(nil)

func NewCratesIOSource(opts CratesIOOptions) *CratesIOSource {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://crates.io/api/v1"
	}
	if opts.DocsRsURL == "" {
		opts.DocsRsURL = "https://docs.rs"
	}
	if opts.MaxPackages == 0 {
		opts.MaxPackages = 1000
	}
	if opts.PerPage == 0 {
		opts.PerPage = 100
	}
	if opts.HTTPTimeout == 0 {
		opts.HTTPTimeout = 30 * time.Second
	}
	return &CratesIOSource{opts: opts}
}

func (s *CratesIOSource) Ecosystem() ecosystem.Ecosystem { return ecosystem.EcoRust }

func (s *CratesIOSource) Kind() ecosystem.SourceType { return ecosystem.SrcPackageDoc }

func (s *CratesIOSource) FetchManifest(ctx context.Context) (*ecosystem.Manifest, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("cratesio: nil Revalidator")
	}
	pages := (s.opts.MaxPackages + s.opts.PerPage - 1) / s.opts.PerPage
	all := make([]ecosystem.ManifestPackage, 0, s.opts.MaxPackages)
	for page := 1; page <= pages; page++ {
		uri := fmt.Sprintf("%s/crates?sort=recent-downloads&per_page=%d&page=%d",
			s.opts.BaseURL, s.opts.PerPage, page)
		ctxPage, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err := s.opts.Revalidator.Fetch(ctxPage, uri, cache.FetchOptions{})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("cratesio: fetch page %d: %w", page, err)
		}
		pkgs, err := parseCratesIOTop(fr.Body)
		if err != nil {
			return nil, fmt.Errorf("cratesio: parse page %d: %w", page, err)
		}
		all = append(all, pkgs...)
		if len(pkgs) < s.opts.PerPage || len(all) >= s.opts.MaxPackages {
			break
		}
	}
	if len(all) > s.opts.MaxPackages {
		all = all[:s.opts.MaxPackages]
	}
	return &ecosystem.Manifest{Packages: all}, nil
}

type cratesIOTopResp struct {
	Crates []struct {
		Name        string `json:"name"`
		MaxVersion  string `json:"max_version"`
		Description string `json:"description"`
		Repository  string `json:"repository"`
	} `json:"crates"`
}

func parseCratesIOTop(body []byte) ([]ecosystem.ManifestPackage, error) {
	var resp cratesIOTopResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]ecosystem.ManifestPackage, 0, len(resp.Crates))
	for _, c := range resp.Crates {
		out = append(out, ecosystem.ManifestPackage{
			Name:                c.Name,
			Versions:            []string{c.MaxVersion},
			LatestStableVersion: c.MaxVersion,
			UpstreamURL:         "https://crates.io/crates/" + c.Name,
			LastUpdated:         now,
		})
	}
	return out, nil
}

type cratesIOCrateResp struct {
	Crate struct {
		Name        string `json:"name"`
		MaxVersion  string `json:"max_version"`
		Description string `json:"description"`
		Homepage    string `json:"homepage"`
		Repository  string `json:"repository"`
		Readme      string `json:"readme"`
	} `json:"crate"`
}

func (s *CratesIOSource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("cratesio: nil Revalidator")
	}
	uri := s.opts.BaseURL + "/crates/" + url.PathEscape(pkg.Name)
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
	cancel()
	if err != nil {
		return nil, fmt.Errorf("cratesio: fetch doc: %w", err)
	}
	var resp cratesIOCrateResp
	if err := json.Unmarshal(fr.Body, &resp); err != nil {
		return nil, fmt.Errorf("cratesio: parse JSON: %w", err)
	}
	sections := []ecosystem.DocSection{
		{
			Kind:        ecosystem.KindModule,
			SymbolPath:  pkg.Name,
			Heading:     "Description",
			Body:        resp.Crate.Description,
			SourceURL:   uri,
			ASTNodeType: "source_file",
		},
		{
			Kind:        ecosystem.KindGuide,
			SymbolPath:  pkg.Name,
			Heading:     "README",
			Body:        resp.Crate.Readme,
			SourceURL:   uri,
			ASTNodeType: "document",
		},
	}
	if s.opts.FetchDocsRs && resp.Crate.MaxVersion != "" {
		docsURI := fmt.Sprintf("%s/%s/%s/%s/index.html",
			s.opts.DocsRsURL, resp.Crate.Name, resp.Crate.MaxVersion, resp.Crate.Name)
		ctx2, cancel2 := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr2, err2 := s.opts.Revalidator.Fetch(ctx2, docsURI, cache.FetchOptions{})
		cancel2()
		if err2 == nil && len(fr2.Body) > 0 {
			sections = append(sections, ecosystem.DocSection{
				Kind:        ecosystem.KindGuide,
				SymbolPath:  pkg.Name,
				Heading:     "docs.rs",
				Body:        string(fr2.Body),
				SourceURL:   docsURI,
				ASTNodeType: "document",
			})
		}
	}
	return &ecosystem.PackageDoc{
		Package:   pkg,
		Version:   resp.Crate.MaxVersion,
		Sections:  sections,
		RawBody:   string(fr.Body),
		SourceURL: uri,
	}, nil
}

func (s *CratesIOSource) FetchChangelog(ctx context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("cratesio: nil Revalidator")
	}

	doc, err := s.FetchPackageDoc(ctx, pkg)
	if err != nil {
		return nil, err
	}
	repo := extractRepoFromCratesIO(doc.RawBody)
	if repo == "" {
		return &ecosystem.Changelog{
			Package:        pkg,
			VersionTo:      version,
			FormatDetected: "not-available",
		}, nil
	}
	rawBase := normalizeGitHubRepo(repo)
	if rawBase == "" {
		return &ecosystem.Changelog{
			Package:        pkg,
			VersionTo:      version,
			FormatDetected: "not-available",
		}, nil
	}

	for _, candidate := range []string{
		"/master/CHANGELOG.md",
		"/main/CHANGELOG.md",
	} {
		uri := rawBase + candidate
		ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
		cancel()
		if err == nil && len(fr.Body) > 0 {
			return &ecosystem.Changelog{
				Package:        pkg,
				VersionTo:      version,
				FormatDetected: "keep-a-changelog",
				RawText:        string(fr.Body),
				SourceURL:      uri,
				Entries:        parseKeepAChangelog(string(fr.Body)),
			}, nil
		}
	}
	return &ecosystem.Changelog{
		Package:        pkg,
		VersionTo:      version,
		FormatDetected: "not-available",
	}, nil
}

func extractRepoFromCratesIO(rawJSON string) string {
	var resp struct {
		Crate struct {
			Repository string `json:"repository"`
		} `json:"crate"`
	}

	_ = json.Unmarshal([]byte(rawJSON), &resp)
	return strings.TrimSpace(resp.Crate.Repository)
}
