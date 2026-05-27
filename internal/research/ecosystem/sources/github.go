//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// internal/research/ecosystem/sources/github.go
//
// handling.
//
// GitHub (https://github.com) is the cross-ecosystem corpus host for the
// top-1000 starred repositories per language. GitHubSource wraps
// the GitHub REST API's /search/repositories endpoint behind the
// ecosystem.Source interface (master plan §3.3) so the ingester
// can pull README + docs/*.md content into ecosystem.db alongside the
// per-language package documentation produced by pkgdev/pypi/npm/cratesio,
// the cross-ecosystem arxiv papers, and the TypeScript-only MDN content.
//
// Instance topology:
// GitHub source is 1-instance-per-ecosystem (4 instances total: one each
// in Go/Python/TypeScript/Rust daemon Source maps), in contrast to the
// pkgdev/pypi/npm/cratesio sources which are 1-instance-total. Each
// per-ecosystem instance filters manifest pulls by language:<eco> (Go,
// Python, TypeScript, Rust) so each ecosystem.db ingests its relevant
// subset. Daemon-init wires 4× GitHubSource instances under
// Source map[Ecosystem]map[SourceType]Source.
//
// FetchManifest paginates /search/repositories?q=language:<eco>+stars:>1000
// at PerPage rows per page until either (a) MaxPackages is reached, or (b)
// upstream returns a short page (< PerPage items, indicating exhausted
// results). Returns []ManifestPackage with Name = "<owner>/<repo>",
// UpstreamURL = canonical github.com URL, LastUpdated = parsed updated_at.
//
// FetchPackageDoc fetches the README.md from raw.githubusercontent.com,
// trying the "main" branch first, falling back to "master" on the first
// fetch error. Additionally fetches up to MaxDocFilesPerRepo files from
// the docs/ subdirectory (discovered via the /repos/{owner}/{repo}/contents/docs
// API). Per-file fetch errors are swallowed silently so a single broken
// docs link does not poison the whole package's ingest. The contents/docs
// listing failure is also swallowed (many repos don't have a docs/
// directory at all).
//
// FetchChangelog tries CHANGELOG.md on main, then master. Returns
// FormatDetected = "keep-a-changelog" + parsed entries on success;
// "not-available" when both branches fail or return an empty body.
//
// Rate-limit handling: GitHub's REST API caps unauthenticated requests at
// 60/hour and authenticated requests at 5000/hour. When the API returns
// HTTP 403 (rate-limit-exceeded) or HTTP 429 (secondary rate-limit) on
// /search/repositories, the implementation backs off with exponential
// delay (RateLimitBackoffBase, doubled per attempt; default 30s, 60s,
// 120s) up to MaxRateLimitRetries (default 3). The backoff respects
// ctx.Done() — a cancelled context aborts the retry loop with ctx.Err()
// rather than blocking. Raw-content fetches from raw.githubusercontent.com
// are NOT rate-limit-throttled by GitHub the same way (separate static
// CDN tier); rate-limit logic is therefore scoped to the search-API path.
//
// Per-source TTL = 1d (already configured in source-ttls.toml from A-10).
//
// All HTTP egress routes via the narrow FetchClient interface declared in
// pkgdev.go (inv-hades-152 + inv-hades-191 — single egress point for the
// research data plane; no direct net/http imports in this package).
//
// Boundary (inv-hades-031): this file MAY import internal/research/cache +
// internal/research/ecosystem (parent) + encoding/json (stdlib). It MUST
// NOT import internal/store, internal/providers, internal/daemon, or any
// net/http symbols.

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

type GitHubOptions struct {
	// Revalidator is the FetchClient used for every HTTP fetch. MUST be
	// non-nil at call time; the constructor does NOT require it (so
	// Ecosystem/Kind identity tests can run without wiring HTTP).
	Revalidator FetchClient

	Ecosystem ecosystem.Ecosystem

	APIBaseURL string

	RawBaseURL string

	Token string

	MaxPackages int

	PerPage int

	HTTPTimeout time.Duration

	StarsMin int

	MaxDocFilesPerRepo int

	RateLimitBackoffBase time.Duration

	MaxRateLimitRetries int
}

type GitHubSource struct {
	opts GitHubOptions
}

var _ ecosystem.Source = (*GitHubSource)(nil)

func NewGitHubSource(opts GitHubOptions) *GitHubSource {
	if opts.APIBaseURL == "" {
		opts.APIBaseURL = "https://api.github.com"
	}
	if opts.RawBaseURL == "" {
		opts.RawBaseURL = "https://raw.githubusercontent.com"
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
	if opts.StarsMin == 0 {
		opts.StarsMin = 1000
	}
	if opts.MaxDocFilesPerRepo == 0 {
		opts.MaxDocFilesPerRepo = 10
	}
	if opts.RateLimitBackoffBase == 0 {
		opts.RateLimitBackoffBase = 30 * time.Second
	}
	if opts.MaxRateLimitRetries == 0 {
		opts.MaxRateLimitRetries = 3
	}
	return &GitHubSource{opts: opts}
}

func (s *GitHubSource) Ecosystem() ecosystem.Ecosystem { return s.opts.Ecosystem }

func (s *GitHubSource) Kind() ecosystem.SourceType { return ecosystem.SrcGitHub }

func (s *GitHubSource) FetchManifest(ctx context.Context) (*ecosystem.Manifest, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("github: nil Revalidator")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lang := ecosystemLanguageFilter(s.opts.Ecosystem)

	pages := (s.opts.MaxPackages + s.opts.PerPage - 1) / s.opts.PerPage
	var all []ecosystem.ManifestPackage
	for page := 1; page <= pages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		q := fmt.Sprintf("language:%s+stars:%s%d", lang, url.QueryEscape(">"), s.opts.StarsMin)
		uri := fmt.Sprintf(
			"%s/search/repositories?q=%s&per_page=%d&sort=stars&order=desc&page=%d",
			s.opts.APIBaseURL, q, s.opts.PerPage, page,
		)
		repos, err := s.fetchSearchPage(ctx, uri)
		if err != nil {
			return nil, err
		}
		all = append(all, repos...)

		if len(all) >= s.opts.MaxPackages || len(repos) < s.opts.PerPage {
			break
		}
	}
	if len(all) > s.opts.MaxPackages {
		all = all[:s.opts.MaxPackages]
	}
	return &ecosystem.Manifest{Packages: all}, nil
}

func ecosystemLanguageFilter(eco ecosystem.Ecosystem) string {
	switch eco {
	case ecosystem.EcoGo:
		return "go"
	case ecosystem.EcoPython:
		return "python"
	case ecosystem.EcoTypeScript:
		return "typescript"
	case ecosystem.EcoRust:
		return "rust"
	}
	return string(eco)
}

func (s *GitHubSource) fetchSearchPage(ctx context.Context, uri string) ([]ecosystem.ManifestPackage, error) {
	maxRetries := s.opts.MaxRateLimitRetries
	for attempt := 0; attempt < maxRetries; attempt++ {

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("github: fetch search: %w", err)
		}

		if fr.HTTPStatusCode == 403 || fr.HTTPStatusCode == 429 {
			waitDur := s.opts.RateLimitBackoffBase * time.Duration(1<<attempt)
			select {
			case <-time.After(waitDur):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if fr.HTTPStatusCode >= 400 {
			return nil, fmt.Errorf("github: HTTP %d: %s", fr.HTTPStatusCode, string(fr.Body))
		}

		return parseGitHubSearchResponse(fr.Body)
	}
	return nil, errors.New("github: rate limit exceeded after max retries")
}

type githubSearchResp struct {
	TotalCount        int  `json:"total_count"`
	IncompleteResults bool `json:"incomplete_results"`
	Items             []struct {
		ID            int       `json:"id"`
		Name          string    `json:"name"`
		FullName      string    `json:"full_name"`
		HTMLURL       string    `json:"html_url"`
		DefaultBranch string    `json:"default_branch"`
		Stargazers    int       `json:"stargazers_count"`
		Language      string    `json:"language"`
		UpdatedAt     time.Time `json:"updated_at"`
	} `json:"items"`
}

func parseGitHubSearchResponse(body []byte) ([]ecosystem.ManifestPackage, error) {
	var resp githubSearchResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]ecosystem.ManifestPackage, 0, len(resp.Items))
	for _, it := range resp.Items {
		out = append(out, ecosystem.ManifestPackage{
			Name:        it.FullName,
			UpstreamURL: it.HTMLURL,
			LastUpdated: it.UpdatedAt,
		})
	}
	return out, nil
}

func (s *GitHubSource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("github: nil Revalidator")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	branch := "main"
	readmeURI := s.opts.RawBaseURL + "/" + pkg.Name + "/" + branch + "/README.md"
	ctxRead, cancelRead := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	frReadme, err := s.opts.Revalidator.Fetch(ctxRead, readmeURI, cache.FetchOptions{})
	cancelRead()
	if err != nil {

		branch = "master"
		readmeURI = s.opts.RawBaseURL + "/" + pkg.Name + "/" + branch + "/README.md"
		ctxAlt, cancelAlt := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		frReadme, err = s.opts.Revalidator.Fetch(ctxAlt, readmeURI, cache.FetchOptions{})
		cancelAlt()
		if err != nil {
			return nil, fmt.Errorf("github: fetch README: %w", err)
		}
	}
	sections := []ecosystem.DocSection{
		{
			Kind:        ecosystem.KindGuide,
			SymbolPath:  pkg.Name,
			Heading:     "README",
			Body:        string(frReadme.Body),
			SourceURL:   readmeURI,
			ASTNodeType: "document",
		},
	}

	if s.opts.MaxDocFilesPerRepo > 0 {
		docsSections := s.fetchDocsDir(ctx, pkg, branch)
		sections = append(sections, docsSections...)
	}
	return &ecosystem.PackageDoc{
		Package:   pkg,
		Version:   branch,
		Sections:  sections,
		RawBody:   string(frReadme.Body),
		SourceURL: readmeURI,
	}, nil
}

type ghContentsEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
}

func (s *GitHubSource) fetchDocsDir(ctx context.Context, pkg ecosystem.PackageRef, branch string) []ecosystem.DocSection {
	listURI := fmt.Sprintf("%s/repos/%s/contents/docs?ref=%s",
		s.opts.APIBaseURL, pkg.Name, branch)
	ctxList, cancelList := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	frList, err := s.opts.Revalidator.Fetch(ctxList, listURI, cache.FetchOptions{})
	cancelList()
	if err != nil {
		return nil
	}
	if frList.HTTPStatusCode >= 400 {
		return nil
	}
	var entries []ghContentsEntry
	if err := json.Unmarshal(frList.Body, &entries); err != nil {
		return nil
	}
	var out []ecosystem.DocSection
	added := 0
	for _, e := range entries {
		if added >= s.opts.MaxDocFilesPerRepo {
			break
		}

		if e.Type != "file" {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name), ".md") {
			continue
		}
		if e.DownloadURL == "" {
			continue
		}
		if err := ctx.Err(); err != nil {

			return out
		}
		ctxFile, cancelFile := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err := s.opts.Revalidator.Fetch(ctxFile, e.DownloadURL, cache.FetchOptions{})
		cancelFile()
		if err != nil {

			continue
		}
		if fr.HTTPStatusCode >= 400 {
			continue
		}
		out = append(out, ecosystem.DocSection{
			Kind:        ecosystem.KindGuide,
			SymbolPath:  pkg.Name + "/" + e.Path,
			Heading:     e.Name,
			Body:        string(fr.Body),
			SourceURL:   e.DownloadURL,
			ASTNodeType: "document",
		})
		added++
	}
	return out
}

func (s *GitHubSource) FetchChangelog(ctx context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error) {
	if s.opts.Revalidator == nil {
		return nil, errors.New("github: nil Revalidator")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, branch := range []string{"main", "master"} {
		uri := s.opts.RawBaseURL + "/" + pkg.Name + "/" + branch + "/CHANGELOG.md"
		ctxFetch, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
		fr, err := s.opts.Revalidator.Fetch(ctxFetch, uri, cache.FetchOptions{})
		cancel()
		if err != nil {
			continue
		}
		if fr.HTTPStatusCode >= 400 {
			continue
		}
		if len(fr.Body) == 0 {
			continue
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
	return &ecosystem.Changelog{
		Package:        pkg,
		VersionTo:      version,
		FormatDetected: "not-available",
	}, nil
}
