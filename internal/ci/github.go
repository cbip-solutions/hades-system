// SPDX-License-Identifier: MIT
// status lookup.
//
// Endpoint pattern (research Topic 3 + GH REST docs):
//
//	GET /repos/{owner}/{repo}/commits?sha={branch}&per_page={n}
//	GET /repos/{owner}/{repo}/commits/{sha}/check-runs
//
// Authentication GITHUB_TOKEN env, with GH_TOKEN fallback for GitHub Actions
// and gh-compatible local shells (read once per call; ok to be unset for public
// repos but rate-limited 60/h vs 5000/h authenticated).
//
// Test injection: $GH_API_BASE_URL overrides the canonical
// https://api.github.com base. ResetHTTPClient() resets the cached
// DefaultHTTPClient (required between subtests that toggle base URL).
// Both hooks are intended for tests only; production path uses the
// canonical URL.
//
// Per-SHA cache (cache.go) populated automatically; only uncached
// SHAs hit the check-runs endpoint. The commits list endpoint is always
// fetched fresh (cheap; one call regardless of cache hits).
package ci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const canonicalGHBase = "https://api.github.com"

var (
	httpClientOnce sync.Once
	httpClient     *http.Client
)

func ResetHTTPClient() {
	httpClientOnce = sync.Once{}
	httpClient = nil
}

func client() *http.Client {
	httpClientOnce.Do(func() {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	})
	return httpClient
}

func baseURL() string {
	if override := os.Getenv("GH_API_BASE_URL"); override != "" {
		return override
	}
	return canonicalGHBase
}

func FetchLastN(ctx context.Context, owner, repo, branch string, n int) ([]CommitStatus, error) {
	commits, err := fetchCommits(ctx, owner, repo, branch, n)
	if err != nil {
		return nil, err
	}
	out := make([]CommitStatus, 0, len(commits))
	for _, sha := range commits {
		if cached, ok := CacheLoad(sha.SHA); ok {
			out = append(out, cached)
			continue
		}
		status, reason, runURL, err := fetchCheckRuns(ctx, owner, repo, sha.SHA)
		if err != nil {
			return nil, err
		}
		c := CommitStatus{
			SHA:    sha.SHA,
			Status: status,
			Reason: reason,
			URL:    runURL,
			Date:   sha.Date,
		}
		// Best-effort cache write (do not fail the gate on cache error).
		_ = CacheStore(c)
		out = append(out, c)
	}
	return out, nil
}

type ghCommitEntry struct {
	SHA  string
	Date time.Time
}

func fetchCommits(ctx context.Context, owner, repo, branch string, n int) ([]ghCommitEntry, error) {
	q := url.Values{}
	q.Set("sha", branch)
	q.Set("per_page", fmt.Sprintf("%d", n))
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits?%s", baseURL(), owner, repo, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ci: build commits request: %w", err)
	}
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ci: fetch commits: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ci: GH commits API %d: %s", resp.StatusCode, string(body))
	}
	var raw []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ci: decode commits: %w", err)
	}
	out := make([]ghCommitEntry, 0, len(raw))
	for _, r := range raw {
		out = append(out, ghCommitEntry{SHA: r.SHA, Date: r.Commit.Committer.Date})
	}
	return out, nil
}

func fetchCheckRuns(ctx context.Context, owner, repo, sha string) (status, reason, runURL string, err error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs", baseURL(), owner, repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("ci: build check-runs request: %w", err)
	}
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client().Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("ci: fetch check-runs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("ci: GH check-runs API %d: %s", resp.StatusCode, string(body))
	}
	var raw struct {
		CheckRuns []struct {
			ID         int64  `json:"id"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			Name       string `json:"name"`
			HTMLURL    string `json:"html_url"`
			Output     struct {
				Title            string `json:"title"`
				Summary          string `json:"summary"`
				Text             string `json:"text"`
				AnnotationsCount int    `json:"annotations_count"`
			} `json:"output"`
		} `json:"check_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", "", "", fmt.Errorf("ci: decode check-runs: %w", err)
	}
	if len(raw.CheckRuns) == 0 {
		return "pending", "no check runs yet", "", nil
	}

	for _, cr := range raw.CheckRuns {
		if cr.Status != "completed" {
			return "pending", "", cr.HTMLURL, nil
		}
		if cr.Conclusion == "failure" || cr.Conclusion == "cancelled" || cr.Conclusion == "timed_out" {
			detailParts := []string{
				strings.TrimSpace(cr.Output.Title),
				strings.TrimSpace(cr.Output.Summary),
				strings.TrimSpace(cr.Output.Text),
			}
			if cr.Output.AnnotationsCount > 0 && cr.ID != 0 {
				annotations, err := fetchCheckRunAnnotations(ctx, owner, repo, cr.ID)
				if err != nil {
					return "", "", "", err
				}
				detailParts = append(detailParts, annotations...)
			}
			return "failure", fmt.Sprintf("%s: %s", cr.Name, strings.Join(nonEmpty(detailParts), " | ")), cr.HTMLURL, nil
		}
	}
	return "success", "", raw.CheckRuns[0].HTMLURL, nil
}

func fetchCheckRunAnnotations(ctx context.Context, owner, repo string, checkRunID int64) ([]string, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/check-runs/%d/annotations", baseURL(), owner, repo, checkRunID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ci: build check-run annotations request: %w", err)
	}
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ci: fetch check-run annotations: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ci: GH check-run annotations API %d: %s", resp.StatusCode, string(body))
	}
	var raw []struct {
		Message    string `json:"message"`
		RawDetails string `json:"raw_details"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ci: decode check-run annotations: %w", err)
	}
	out := make([]string, 0, len(raw)*2)
	for _, a := range raw {
		out = append(out, strings.TrimSpace(a.Message), strings.TrimSpace(a.RawDetails))
	}
	return nonEmpty(out), nil
}

func githubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
