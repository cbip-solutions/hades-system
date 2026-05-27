//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// internal/research/ecosystem/sources/pypi.go
//
// ecosystem. Implements ecosystem.Source (master plan §3.3).
//
// FetchManifest dual-path:
// - Libraries.io (`https://libraries.io/api/search?platforms=PyPI&...`)
// when PyPIOptions.LibrariesIOAPIKey is set; paginated stars-ranked
// across MaxPackages (default 5000) at PerPage (default 100).
// - hugovk top-PyPI-packages community BigQuery mirror
// (`https://hugovk.github.io/top-pypi-packages/top-pypi-packages-30-days.json`)
// fallback when no API key is configured.
//
// FetchPackageDoc fetches `https://pypi.org/pypi/<pkg>/json` and emits a
// minimum-viable PackageDoc with two DocSections: Summary (KindModule) and
// Description (KindGuide). The full PyPI JSON body is stashed in RawBody
// for downstream chunker re-parsing in chunker.go.
//
// FetchChangelog discovers a GitHub repo from `project_urls.Source` (or a
// GitHub Homepage when Source is absent), transforms to raw.githubusercontent.com
// and tries `main/CHANGELOG.md` first, then `main/doc/release/upcoming_changes/README.md`
// (numpy-style). When neither succeeds OR no Source URL is derivable,
// returns an empty Changelog with FormatDetected = "not-available" (NOT
// an error — Python ecosystem has no unified changelog convention).
//
// All HTTP egress routes via the narrow FetchClient interface declared
// in pkgdev.go; no direct net/http imports (invariant + invariant).
// Per-source TTL = 1d (registered in source-ttls.toml at A-10).
//
// Boundary (invariant): this file MAY import internal/research/cache
// + internal/research/ecosystem (parent). It MUST NOT import internal/store
// or internal/providers. Verified by `go test -run TestEcosystemBoundary`
// and vet analyzer no_web_in_ecosystem.
//
// Concurrency PyPISource is safe for concurrent use from multiple
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

type PyPIOptions struct {
	// Revalidator is the FetchClient used for every HTTP fetch. MUST be
	// non-nil when any Fetch* method is called; constructor does NOT
	// require it so Ecosystem/Kind identity tests can run without HTTP wiring.
	Revalidator FetchClient

	BaseURL string

	LibrariesIOBaseURL string

	LibrariesIOAPIKey string

	FallbackTopURL string

	MaxPackages int

	// PerPage is the libraries.io pagination size. Default 100. Libraries.io
	// per-page max is 100; do not set higher without testing API tolerance.
	PerPage int

	HTTPTimeout time.Duration
}

type PyPISource struct {
	opts PyPIOptions
}

var _ ecosystem.Source = (*PyPISource)(nil)

func NewPyPISource(opts PyPIOptions) *PyPISource {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://pypi.org/pypi"
	}
	if opts.LibrariesIOBaseURL == "" {
		opts.LibrariesIOBaseURL = "https://libraries.io/api"
	}
	if opts.FallbackTopURL == "" {
		opts.FallbackTopURL = "https://hugovk.github.io/top-pypi-packages/top-pypi-packages-30-days.json"
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
	return &PyPISource{opts: opts}
}

func (s *PyPISource) Ecosystem() ecosystem.Ecosystem { return ecosystem.EcoPython }

func (s *PyPISource) Kind() ecosystem.SourceType { return ecosystem.SrcPackageDoc }

func (s *PyPISource) FetchManifest(ctx context.Context) (*ecosystem.Manifest, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("pypi: nil Revalidator")
	}
	if s.opts.LibrariesIOAPIKey != "" {
		return s.fetchManifestLibrariesIO(ctx)
	}
	return s.fetchManifestFallback(ctx)
}

func (s *PyPISource) fetchManifestLibrariesIO(ctx context.Context) (*ecosystem.Manifest, error) {
	pages := (s.opts.MaxPackages + s.opts.PerPage - 1) / s.opts.PerPage
	all := make([]ecosystem.ManifestPackage, 0, s.opts.MaxPackages)
	for page := 1; page <= pages; page++ {
		uri := fmt.Sprintf("%s/search?platforms=PyPI&sort=stars&per_page=%d&page=%d",
			s.opts.LibrariesIOBaseURL, s.opts.PerPage, page)
		ctxPage, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err := s.opts.Revalidator.Fetch(ctxPage, uri, cache.FetchOptions{})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("pypi: libraries.io page %d: %w", page, err)
		}
		pkgs, err := parseLibrariesIO(fr.Body)
		if err != nil {
			return nil, fmt.Errorf("pypi: parse libraries.io page %d: %w", page, err)
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

type librariesIOEntry struct {
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	Stars            int    `json:"stars"`
	LatestReleaseNum string `json:"latest_release_number"`
}

func parseLibrariesIO(body []byte) ([]ecosystem.ManifestPackage, error) {
	var entries []librariesIOEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]ecosystem.ManifestPackage, 0, len(entries))
	for _, e := range entries {
		out = append(out, ecosystem.ManifestPackage{
			Name:                e.Name,
			Versions:            []string{e.LatestReleaseNum},
			LatestStableVersion: e.LatestReleaseNum,
			UpstreamURL:         "https://pypi.org/project/" + e.Name + "/",
			LastUpdated:         now,
		})
	}
	return out, nil
}

func (s *PyPISource) fetchManifestFallback(ctx context.Context) (*ecosystem.Manifest, error) {
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, s.opts.FallbackTopURL, cache.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("pypi: fallback fetch: %w", err)
	}
	type hugovkEntry struct {
		Project       string `json:"project"`
		DownloadCount int    `json:"download_count"`
	}
	type hugovkResponse struct {
		Rows []hugovkEntry `json:"rows"`
	}
	var resp hugovkResponse
	if err := json.Unmarshal(fr.Body, &resp); err != nil {
		return nil, fmt.Errorf("pypi: parse fallback: %w", err)
	}
	now := time.Now()
	out := make([]ecosystem.ManifestPackage, 0, len(resp.Rows))
	for i, r := range resp.Rows {
		if i >= s.opts.MaxPackages {
			break
		}
		out = append(out, ecosystem.ManifestPackage{
			Name:        r.Project,
			UpstreamURL: "https://pypi.org/project/" + r.Project + "/",
			LastUpdated: now,
		})
	}
	return &ecosystem.Manifest{Packages: out}, nil
}

func (s *PyPISource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("pypi: nil Revalidator")
	}
	uri := s.opts.BaseURL + "/" + url.PathEscape(pkg.Name) + "/json"
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("pypi: fetch doc: %w", err)
	}
	var resp pypiResp
	if err := json.Unmarshal(fr.Body, &resp); err != nil {
		return nil, fmt.Errorf("pypi: parse JSON: %w", err)
	}
	projectPageURL := "https://pypi.org/project/" + pkg.Name + "/"
	sections := []ecosystem.DocSection{
		{
			Kind:        ecosystem.KindModule,
			SymbolPath:  pkg.Name,
			Heading:     "Summary",
			Body:        resp.Info.Summary,
			SourceURL:   projectPageURL,
			ASTNodeType: "module",
		},
		{
			Kind:        ecosystem.KindGuide,
			SymbolPath:  pkg.Name,
			Heading:     "Description",
			Body:        resp.Info.Description,
			SourceURL:   projectPageURL,
			ASTNodeType: "document",
		},
	}
	return &ecosystem.PackageDoc{
		Package:   pkg,
		Version:   resp.Info.Version,
		Sections:  sections,
		RawBody:   string(fr.Body),
		SourceURL: uri,
	}, nil
}

type pypiInfo struct {
	Name                   string            `json:"name"`
	Version                string            `json:"version"`
	Summary                string            `json:"summary"`
	Description            string            `json:"description"`
	DescriptionContentType string            `json:"description_content_type"`
	Author                 string            `json:"author"`
	License                string            `json:"license"`
	RequiresPython         string            `json:"requires_python"`
	Classifiers            []string          `json:"classifiers"`
	ProjectURLs            map[string]string `json:"project_urls"`
}

type pypiResp struct {
	Info pypiInfo `json:"info"`
}

func (s *PyPISource) FetchChangelog(ctx context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("pypi: nil Revalidator")
	}

	doc, err := s.FetchPackageDoc(ctx, pkg)
	if err != nil {
		return nil, err
	}
	source := extractSourceURL(doc.RawBody)
	if source == "" {
		return &ecosystem.Changelog{
			Package:        pkg,
			VersionTo:      version,
			FormatDetected: "not-available",
		}, nil
	}

	rawBase := strings.Replace(source, "github.com", "raw.githubusercontent.com", 1)
	uri := rawBase + "/main/CHANGELOG.md"
	ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
	cancel()
	if err != nil {

		altURI := rawBase + "/main/doc/release/upcoming_changes/README.md"
		ctx2, cancel2 := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err = s.opts.Revalidator.Fetch(ctx2, altURI, cache.FetchOptions{})
		cancel2()
		if err != nil {
			return &ecosystem.Changelog{
				Package:        pkg,
				VersionTo:      version,
				FormatDetected: "not-available",
			}, nil
		}
		uri = altURI
	}
	return &ecosystem.Changelog{
		Package:        pkg,
		VersionTo:      version,
		FormatDetected: "keep-a-changelog",
		RawText:        string(fr.Body),
		SourceURL:      uri,
		Entries:        parseKeepAChangelog(string(fr.Body)),
	}, nil
}

func extractSourceURL(rawJSON string) string {
	var resp struct {
		Info struct {
			ProjectURLs map[string]string `json:"project_urls"`
		} `json:"info"`
	}
	_ = json.Unmarshal([]byte(rawJSON), &resp)
	if v, ok := resp.Info.ProjectURLs["Source"]; ok && v != "" {
		return v
	}
	if v, ok := resp.Info.ProjectURLs["Homepage"]; ok {
		if strings.Contains(v, "github.com") {
			return v
		}
	}
	return ""
}

func parseKeepAChangelog(s string) []ecosystem.ChangelogEntry {
	var entries []ecosystem.ChangelogEntry
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			body := strings.TrimSpace(t[2:])
			entries = append(entries, ecosystem.ChangelogEntry{
				ChangeType: classifyChangeType("", body),
				Summary:    body,
			})
		}
	}
	return entries
}
