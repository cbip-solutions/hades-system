// SPDX-License-Identifier: MIT
// web_search.go — DDG + optional Firecrawl, with cache hit/miss path.
//
// Two paths:
// 1. DDG (DuckDuckGo) for bulk hits via the daemon-routed search proxy
// OR direct duckduckgo.com.
// 2. Firecrawl for full-page extraction when extract=true (gated by
// doctrine; default off).
//
// Cache hit/miss is the first stage: webSearchCacheKey(query, max)
// → s.cache.Get; on hit, return; on miss, run the search + s.cache.Set.
//
// inv-hades-076: budget.PreCall is called BEFORE the cache lookup at the
// handler level (server.go handleWebSearch); the WebSearch backend
// itself is invoked only after the gate passes.
package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type WebSearchOptions struct {
	DDGURL string

	FirecrawlURL string

	HTTPClient *http.Client

	Cache CacheClient

	CacheTTL time.Duration

	RequestTimeout time.Duration
}

type WebSearch struct {
	opts WebSearchOptions
}

func NewWebSearch(opts WebSearchOptions) *WebSearch {
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.CacheTTL == 0 {
		opts.CacheTTL = 7 * 24 * time.Hour
	}
	if opts.RequestTimeout == 0 {
		opts.RequestTimeout = 30 * time.Second
	}
	return &WebSearch{opts: opts}
}

func (w *WebSearch) Search(ctx context.Context, query string, max int) ([]SourceHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("research/web_search: query is empty")
	}
	if max <= 0 {
		max = 10
	}
	hash := webSearchCacheKey(query, max)

	if w.opts.Cache != nil {
		if entry, ok, err := w.opts.Cache.Get(ctx, hash); err == nil && ok {
			var hits []SourceHit
			if uerr := json.Unmarshal(entry.Response, &hits); uerr == nil {
				return capHits(hits, max), nil
			}

		}
	}

	hits, err := w.callDDG(ctx, query, max)
	if err != nil {
		return nil, err
	}
	hits = capHits(hits, max)

	// Cache write (best-effort: errors do not fail the search).
	if w.opts.Cache != nil {
		body, _ := json.Marshal(hits)
		_ = w.opts.Cache.Set(ctx, hash, CacheEntry{
			Hash:     hash,
			Response: body,
			StoredAt: time.Now().Unix(),
			TTLUnix:  time.Now().Add(w.opts.CacheTTL).Unix(),
		}, int64(w.opts.CacheTTL.Seconds()))
	}
	return hits, nil
}

func (w *WebSearch) SearchWithExtraction(ctx context.Context, query string, max int) ([]SourceHit, error) {
	hits, err := w.Search(ctx, query, max)
	if err != nil {
		return nil, err
	}
	if w.opts.FirecrawlURL == "" {
		return hits, nil
	}
	enriched := make([]SourceHit, 0, len(hits))
	for _, h := range hits {
		excerpt, ferr := w.callFirecrawl(ctx, h.URL)
		if ferr != nil {

			h.Excerpt = "[firecrawl-failed: " + sanitizeErrForExcerpt(ferr) + "] " + h.Excerpt
		} else if excerpt != "" {
			h.Excerpt = excerpt
		}
		enriched = append(enriched, h)
	}
	return enriched, nil
}

func sanitizeErrForExcerpt(err error) string {
	const maxErrLen = 200
	s := err.Error()
	if len(s) > maxErrLen {
		s = s[:maxErrLen] + "..."
	}

	return fmt.Sprintf("%q", s)
}

func (w *WebSearch) callDDG(ctx context.Context, query string, max int) ([]SourceHit, error) {
	u, err := url.Parse(w.opts.DDGURL)
	if err != nil {
		return nil, fmt.Errorf("ddg url: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("max", fmt.Sprintf("%d", max))
	u.RawQuery = q.Encode()

	if w.opts.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.opts.RequestTimeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("ddg new req: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := w.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ddg do: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ddg status %d: %s", resp.StatusCode, snippet(body))
	}
	var envelope struct {
		Results []SourceHit `json:"results"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("ddg parse: %w", err)
	}
	for i := range envelope.Results {
		if envelope.Results[i].Source == "" {
			envelope.Results[i].Source = "ddg"
		}
	}
	return envelope.Results, nil
}

func (w *WebSearch) callFirecrawl(ctx context.Context, target string) (string, error) {
	if w.opts.FirecrawlURL == "" {
		return "", nil
	}
	u, err := url.Parse(w.opts.FirecrawlURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("url", target)
	u.RawQuery = q.Encode()

	if w.opts.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.opts.RequestTimeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := w.opts.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("firecrawl status %d", resp.StatusCode)
	}
	var envelope struct {
		Markdown string `json:"markdown"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("firecrawl parse: %w", err)
	}
	if envelope.Markdown != "" {
		return envelope.Markdown, nil
	}
	return envelope.Text, nil
}

func webSearchCacheKey(query string, max int) string {
	h := sha256.New()
	h.Write([]byte(query))
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%d", max)))
	return hex.EncodeToString(h.Sum(nil))
}

func capHits(hits []SourceHit, max int) []SourceHit {
	if len(hits) > max {
		return hits[:max]
	}
	return hits
}

func snippet(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}
