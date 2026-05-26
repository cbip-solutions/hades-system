// SPDX-License-Identifier: MIT
// arxiv.go — export.arxiv.org REST API + XML (Atom) parse + cache.
//
// ArXiv exposes a REST search at https://export.arxiv.org/api/query
// returning Atom XML. We parse just enough to extract per-entry title,
// abstract, authors, primary URL, and updated date. Cache hit/miss is
// the same shape as web_search: sha256(query|max|sortBy) → CacheClient.
package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ArxivOptions struct {
	BaseURL string

	HTTPClient *http.Client

	Cache CacheClient

	CacheTTL time.Duration

	RequestTimeout time.Duration
}

type Arxiv struct {
	opts ArxivOptions
}

func NewArxiv(opts ArxivOptions) *Arxiv {
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.BaseURL == "" {
		opts.BaseURL = "https://export.arxiv.org/api/query"
	}
	if opts.CacheTTL == 0 {
		opts.CacheTTL = 7 * 24 * time.Hour
	}
	if opts.RequestTimeout == 0 {
		opts.RequestTimeout = 30 * time.Second
	}
	return &Arxiv{opts: opts}
}

type arxivFeed struct {
	XMLName xml.Name     `xml:"feed"`
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	ID      string        `xml:"id"`
	Updated string        `xml:"updated"`
	Title   string        `xml:"title"`
	Summary string        `xml:"summary"`
	Authors []arxivAuthor `xml:"author"`
	Links   []arxivLink   `xml:"link"`
}

type arxivAuthor struct {
	Name string `xml:"name"`
}

type arxivLink struct {
	Href  string `xml:"href,attr"`
	Rel   string `xml:"rel,attr"`
	Title string `xml:"title,attr"`
}

func (a *Arxiv) Search(ctx context.Context, query string, max int, sortBy string) ([]SourceHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("research/arxiv: query is empty")
	}
	if max <= 0 {
		max = 10
	}
	if sortBy == "" {
		sortBy = "relevance"
	}
	if sortBy != "relevance" && sortBy != "lastUpdatedDate" && sortBy != "submittedDate" {
		return nil, fmt.Errorf("research/arxiv: invalid sort_by=%q", sortBy)
	}
	hash := arxivCacheKey(query, max, sortBy)

	if a.opts.Cache != nil {
		if entry, ok, err := a.opts.Cache.Get(ctx, hash); err == nil && ok {
			var hits []SourceHit
			if uerr := json.Unmarshal(entry.Response, &hits); uerr == nil {
				return capHits(hits, max), nil
			}
		}
	}

	hits, err := a.fetch(ctx, query, max, sortBy)
	if err != nil {
		return nil, err
	}
	hits = capHits(hits, max)

	if a.opts.Cache != nil {
		body, _ := json.Marshal(hits)
		_ = a.opts.Cache.Set(ctx, hash, CacheEntry{
			Hash:     hash,
			Response: body,
			StoredAt: time.Now().Unix(),
			TTLUnix:  time.Now().Add(a.opts.CacheTTL).Unix(),
		}, int64(a.opts.CacheTTL.Seconds()))
	}
	return hits, nil
}

func (a *Arxiv) fetch(ctx context.Context, query string, max int, sortBy string) ([]SourceHit, error) {
	u, err := url.Parse(a.opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("arxiv url: %w", err)
	}
	q := u.Query()
	q.Set("search_query", query)
	q.Set("max_results", fmt.Sprintf("%d", max))
	q.Set("sortBy", sortBy)
	q.Set("sortOrder", "descending")
	u.RawQuery = q.Encode()

	if a.opts.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.opts.RequestTimeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("arxiv new req: %w", err)
	}
	req.Header.Set("Accept", "application/atom+xml")

	resp, err := a.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arxiv do: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("arxiv status %d: %s", resp.StatusCode, snippet(body))
	}

	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("arxiv parse: %w", err)
	}
	hits := make([]SourceHit, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		hit := SourceHit{
			Source:  "arxiv",
			URL:     e.ID,
			Title:   strings.TrimSpace(e.Title),
			Excerpt: cleanWhitespace(e.Summary),
		}

		for _, l := range e.Links {
			if l.Rel == "alternate" && l.Href != "" {
				hit.URL = l.Href
				break
			}
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func arxivCacheKey(query string, max int, sortBy string) string {
	h := sha256.New()
	h.Write([]byte(query))
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%d", max)))
	h.Write([]byte("|"))
	h.Write([]byte(sortBy))
	return hex.EncodeToString(h.Sum(nil))
}

func cleanWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
