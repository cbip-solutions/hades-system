// SPDX-License-Identifier: MIT
// github_search.go — GitHub repository search via go-github + auth.
//
// Calls api.github.com via the go-github v66 client with optional
// bearer-token auth (production: macOS Keychain via the
// `gh:hades-system:github-search-token` slot; tests inject directly).
// Cache hit/miss is the same shape as web_search and arxiv:
// sha256(query|language|stars_min) → CacheClient.
//
// Build the final product: go-github exposes clean SearchOptions; we
// pass the hit list straight through with translation to SourceHit.
package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v66/github"
)

type GitHubSearchOptions struct {
	AuthToken string

	HTTPClient *http.Client

	BaseURL string

	Cache CacheClient

	CacheTTL time.Duration
}

type GitHubSearch struct {
	opts   GitHubSearchOptions
	client *github.Client
}

func NewGitHubSearch(opts GitHubSearchOptions) *GitHubSearch {
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.CacheTTL == 0 {
		opts.CacheTTL = 7 * 24 * time.Hour
	}

	cli := github.NewClient(opts.HTTPClient)
	if opts.AuthToken != "" {
		cli = cli.WithAuthToken(opts.AuthToken)
	}
	if opts.BaseURL != "" {
		var err error
		cli, err = cli.WithEnterpriseURLs(opts.BaseURL, opts.BaseURL)
		if err != nil {

			cli = github.NewClient(opts.HTTPClient)
		}
	}
	return &GitHubSearch{opts: opts, client: cli}
}

func (g *GitHubSearch) Search(ctx context.Context, query, language string, starsMin int) ([]SourceHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("research/github_search: query is empty")
	}
	hash := githubCacheKey(query, language, starsMin)

	if g.opts.Cache != nil {
		if entry, ok, err := g.opts.Cache.Get(ctx, hash); err == nil && ok {
			var hits []SourceHit
			if uerr := json.Unmarshal(entry.Response, &hits); uerr == nil {
				return hits, nil
			}
		}
	}

	qParts := []string{query}
	if language != "" {
		qParts = append(qParts, "language:"+language)
	}
	if starsMin > 0 {
		qParts = append(qParts, fmt.Sprintf("stars:>=%d", starsMin))
	}
	dslQuery := strings.Join(qParts, " ")

	opts := &github.SearchOptions{
		Sort:        "stars",
		Order:       "desc",
		ListOptions: github.ListOptions{PerPage: 30},
	}
	result, _, err := g.client.Search.Repositories(ctx, dslQuery, opts)
	if err != nil {
		return nil, fmt.Errorf("github search: %w", err)
	}
	hits := make([]SourceHit, 0, len(result.Repositories))
	for _, repo := range result.Repositories {
		stars := 0
		if repo.StargazersCount != nil {
			stars = *repo.StargazersCount
		}
		hit := SourceHit{
			Source:  "github",
			URL:     deref(repo.HTMLURL),
			Title:   deref(repo.FullName),
			Excerpt: deref(repo.Description),
			Score:   float64(stars),
		}
		hits = append(hits, hit)
	}

	if g.opts.Cache != nil {
		body, _ := json.Marshal(hits)
		_ = g.opts.Cache.Set(ctx, hash, CacheEntry{
			Hash:     hash,
			Response: body,
			StoredAt: time.Now().Unix(),
			TTLUnix:  time.Now().Add(g.opts.CacheTTL).Unix(),
		}, int64(g.opts.CacheTTL.Seconds()))
	}
	return hits, nil
}

func githubCacheKey(query, language string, starsMin int) string {
	h := sha256.New()
	h.Write([]byte(query))
	h.Write([]byte("|"))
	h.Write([]byte(language))
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%d", starsMin)))
	return hex.EncodeToString(h.Sum(nil))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
