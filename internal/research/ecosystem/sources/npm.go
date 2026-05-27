//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// internal/research/ecosystem/sources/npm.go
//
// JavaScript ecosystem. Implements ecosystem.Source (master plan §3.3).
//
// FetchManifest queries the npms.io v2 search endpoint
// (`https://api.npms.io/v2/search?q=keywords:javascript&size=250&from=N`)
// which returns a quality+popularity-weighted ranking of npm packages.
// Pagination is driven by from-offsets at PerPage (default 250) up to
// MaxPackages (default 5000). KeywordFilter (default "javascript") is
// overridable so the same source code can drive a typescript-keyword scan.
//
// FetchPackageDoc fetches `https://registry.npmjs.org/<pkg>` and emits a
// PackageDoc with two DocSections: Description (KindModule, from the
// registry's "description" field) and README (KindGuide, from the
// registry's "readme" markdown body). The full registry JSON is preserved
// in RawBody for downstream chunker re-parsing in chunker.go
// (which applies tree-sitter TypeScript / Markdown grammars).
//
// FetchChangelog derives a GitHub repo URL from `repository.url` in the
// registry JSON, normalizes git+https / git:// prefixes to https://, and
// then probes three known changelog locations in order:
// 1. master/History.md (Express + many older npm packages)
// 2. master/CHANGELOG.md (Keep-a-Changelog on master)
// 3. main/CHANGELOG.md (Keep-a-Changelog on main — modern default)
// First hit wins. When none succeed OR no repository URL is derivable
// OR the repo is non-GitHub (e.g. self-hosted GitLab), returns a non-nil
// Changelog with FormatDetected = "not-available" (NOT an error — npm
// packages routinely lack changelogs).
//
// The npm ecosystem maps to ONE release ecosystem (`typescript`) per
// master §0.2: TS + JS packages share the npm registry and release's
// chunker uses TypeScript grammar for both. Ecosystem() returns
// EcoTypeScript; the KeywordFilter knob lets the ingester run two
// keyword-filtered passes if desired without changing the source code.
//
// All HTTP egress routes via the narrow FetchClient interface declared
// in pkgdev.go; no direct net/http imports (inv-hades-152 + inv-hades-191).
// Per-source TTL = 1d (registered in source-ttls.toml at A-10).
//
// Boundary (inv-hades-031): this file MAY import internal/research/cache
// + internal/research/ecosystem (parent). It MUST NOT import internal/store
// or internal/providers. Verified by `go test -run TestEcosystemBoundary`
// and vet analyzer no_web_in_ecosystem.
//
// Concurrency NpmSource is safe for concurrent use from multiple
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

type NpmOptions struct {
	// Revalidator is the FetchClient used for every HTTP fetch. MUST be
	// non-nil when any Fetch* method is called; constructor does NOT
	// require it so Ecosystem/Kind identity tests can run without HTTP wiring.
	Revalidator FetchClient

	RegistryURL string

	NpmsURL string

	MaxPackages int

	PerPage int

	HTTPTimeout time.Duration

	KeywordFilter string
}

type NpmSource struct {
	opts NpmOptions
}

var _ ecosystem.Source = (*NpmSource)(nil)

func NewNpmSource(opts NpmOptions) *NpmSource {
	if opts.RegistryURL == "" {
		opts.RegistryURL = "https://registry.npmjs.org"
	}
	if opts.NpmsURL == "" {
		opts.NpmsURL = "https://api.npms.io/v2"
	}
	if opts.MaxPackages == 0 {
		opts.MaxPackages = 5000
	}
	if opts.PerPage == 0 {
		opts.PerPage = 250
	}
	if opts.HTTPTimeout == 0 {
		opts.HTTPTimeout = 30 * time.Second
	}
	if opts.KeywordFilter == "" {
		opts.KeywordFilter = "javascript"
	}
	return &NpmSource{opts: opts}
}

func (s *NpmSource) Ecosystem() ecosystem.Ecosystem { return ecosystem.EcoTypeScript }

func (s *NpmSource) Kind() ecosystem.SourceType { return ecosystem.SrcPackageDoc }

func (s *NpmSource) FetchManifest(ctx context.Context) (*ecosystem.Manifest, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("npm: nil Revalidator")
	}
	pages := (s.opts.MaxPackages + s.opts.PerPage - 1) / s.opts.PerPage
	all := make([]ecosystem.ManifestPackage, 0, s.opts.MaxPackages)
	for page := 0; page < pages; page++ {
		uri := fmt.Sprintf("%s/search?q=keywords:%s&size=%d&from=%d",
			s.opts.NpmsURL, s.opts.KeywordFilter, s.opts.PerPage, page*s.opts.PerPage)
		ctxPage, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err := s.opts.Revalidator.Fetch(ctxPage, uri, cache.FetchOptions{})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("npm: fetch npms page %d: %w", page, err)
		}
		pkgs, err := parseNpmsResults(fr.Body)
		if err != nil {
			return nil, fmt.Errorf("npm: parse npms page %d: %w", page, err)
		}
		all = append(all, pkgs...)
		if len(pkgs) == 0 || len(all) >= s.opts.MaxPackages {
			break
		}
	}
	if len(all) > s.opts.MaxPackages {
		all = all[:s.opts.MaxPackages]
	}
	return &ecosystem.Manifest{Packages: all}, nil
}

type npmsResults struct {
	Results []struct {
		Package struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
			Links       struct {
				NPM string `json:"npm"`
			} `json:"links"`
		} `json:"package"`
	} `json:"results"`
}

func parseNpmsResults(body []byte) ([]ecosystem.ManifestPackage, error) {
	var resp npmsResults
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]ecosystem.ManifestPackage, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, ecosystem.ManifestPackage{
			Name:                r.Package.Name,
			Versions:            []string{r.Package.Version},
			LatestStableVersion: r.Package.Version,
			UpstreamURL:         r.Package.Links.NPM,
			LastUpdated:         now,
		})
	}
	return out, nil
}

func (s *NpmSource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("npm: nil Revalidator")
	}
	uri := s.opts.RegistryURL + "/" + url.PathEscape(pkg.Name)
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("npm: fetch doc: %w", err)
	}
	var resp npmRegistryResp
	if err := json.Unmarshal(fr.Body, &resp); err != nil {
		return nil, fmt.Errorf("npm: parse JSON: %w", err)
	}
	latest := resp.DistTags["latest"]
	sections := []ecosystem.DocSection{
		{
			Kind:        ecosystem.KindModule,
			SymbolPath:  pkg.Name,
			Heading:     "Description",
			Body:        resp.Description,
			SourceURL:   uri,
			ASTNodeType: "module",
		},
		{
			Kind:        ecosystem.KindGuide,
			SymbolPath:  pkg.Name,
			Heading:     "README",
			Body:        resp.Readme,
			SourceURL:   uri,
			ASTNodeType: "document",
		},
	}
	return &ecosystem.PackageDoc{
		Package:   pkg,
		Version:   latest,
		Sections:  sections,
		RawBody:   string(fr.Body),
		SourceURL: uri,
	}, nil
}

type npmRegistryResp struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	DistTags    map[string]string `json:"dist-tags"`
	Readme      string            `json:"readme"`
	Repository  struct {
		URL string `json:"url"`
	} `json:"repository"`
}

func (s *NpmSource) FetchChangelog(ctx context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("npm: nil Revalidator")
	}

	doc, err := s.FetchPackageDoc(ctx, pkg)
	if err != nil {
		return nil, err
	}
	repo := extractRepoURL(doc.RawBody)
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
		"/master/History.md",
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
				FormatDetected: "github-release",
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

func extractRepoURL(rawJSON string) string {
	var resp struct {
		Repository struct {
			URL string `json:"url"`
		} `json:"repository"`
	}
	_ = json.Unmarshal([]byte(rawJSON), &resp)
	return resp.Repository.URL
}

func normalizeGitHubRepo(repo string) string {
	if repo == "" {
		return ""
	}
	r := strings.TrimPrefix(repo, "git+")
	if strings.HasPrefix(r, "git://") {
		r = "https://" + strings.TrimPrefix(r, "git://")
	}
	r = strings.TrimSuffix(r, ".git")
	if !strings.Contains(r, "github.com") {
		return ""
	}
	r = strings.Replace(r, "github.com", "raw.githubusercontent.com", 1)
	if !strings.HasPrefix(r, "https://") {
		return ""
	}
	return r
}
