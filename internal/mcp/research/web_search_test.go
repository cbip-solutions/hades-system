package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebSearchHappyPath(t *testing.T) {
	ddg := newDDGFake(t, []SourceHit{
		{Source: "ddg", URL: "https://example.com/a", Title: "A", Excerpt: "x"},
		{Source: "ddg", URL: "https://example.com/b", Title: "B", Excerpt: "y"},
	})
	defer ddg.Close()

	cache := &recordingCache{}
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     ddg.URL(),
		HTTPClient: http.DefaultClient,
		Cache:      cache,
	})
	hits, err := w.Search(context.Background(), "claude code", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits=%d, want 2", len(hits))
	}
	if cache.gets != 1 || cache.sets != 1 {
		t.Errorf("cache gets=%d sets=%d, want 1/1", cache.gets, cache.sets)
	}
}

func TestWebSearchCacheHit(t *testing.T) {
	cached := []SourceHit{{Source: "ddg", URL: "https://cached.example/", Title: "Cached"}}
	body, _ := json.Marshal(cached)
	cache := &recordingCache{
		seedEntry: CacheEntry{Hash: "ignored-by-fake", Response: body, TTLUnix: 9999999999},
		seedHit:   true,
	}
	ddg := newDDGFake(t, nil)
	defer ddg.Close()

	w := NewWebSearch(WebSearchOptions{
		DDGURL:     ddg.URL(),
		HTTPClient: http.DefaultClient,
		Cache:      cache,
	})
	got, err := w.Search(context.Background(), "x", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://cached.example/" {
		t.Fatalf("got = %v", got)
	}
	if ddg.calls != 0 {
		t.Errorf("DDG was called %d times despite cache hit", ddg.calls)
	}
}

func TestWebSearchDDGTransport500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cache := &recordingCache{}
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     srv.URL,
		HTTPClient: http.DefaultClient,
		Cache:      cache,
	})
	if _, err := w.Search(context.Background(), "x", 5); err == nil {
		t.Fatal("expected error on DDG 500")
	}
	if cache.sets != 0 {
		t.Errorf("cache.sets=%d, want 0", cache.sets)
	}
}

func TestWebSearchEmptyQuery(t *testing.T) {
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     "http://unused",
		HTTPClient: http.DefaultClient,
		Cache:      &recordingCache{},
	})
	if _, err := w.Search(context.Background(), "", 10); err == nil {
		t.Fatal("expected error on empty query")
	}
}

func TestWebSearchMaxResultsRespected(t *testing.T) {
	ddg := newDDGFake(t, []SourceHit{
		{URL: "https://a"}, {URL: "https://b"}, {URL: "https://c"}, {URL: "https://d"},
	})
	defer ddg.Close()
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     ddg.URL(),
		HTTPClient: http.DefaultClient,
		Cache:      &recordingCache{},
	})
	hits, err := w.Search(context.Background(), "q", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 2 {
		t.Errorf("len(hits)=%d, want ≤ 2", len(hits))
	}
}

func TestCacheHashStable(t *testing.T) {
	a := webSearchCacheKey("q1", 10)
	b := webSearchCacheKey("q1", 10)
	if a != b {
		t.Errorf("expected stable hash; got %s vs %s", a, b)
	}
	if c := webSearchCacheKey("q1", 11); c == a {
		t.Errorf("hash collision across max: %s", c)
	}
}

func TestWebSearchInvalidDDGURL(t *testing.T) {
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     "://not-a-url",
		HTTPClient: http.DefaultClient,
		Cache:      &recordingCache{},
	})
	if _, err := w.Search(context.Background(), "q", 1); err == nil {
		t.Fatal("expected error on bad URL")
	}
}

func TestWebSearchDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     srv.URL,
		HTTPClient: http.DefaultClient,
		Cache:      &recordingCache{},
	})
	if _, err := w.Search(context.Background(), "q", 1); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestWebSearchNoCache(t *testing.T) {
	ddg := newDDGFake(t, []SourceHit{{URL: "https://no-cache"}})
	defer ddg.Close()
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     ddg.URL(),
		HTTPClient: http.DefaultClient,
	})
	hits, err := w.Search(context.Background(), "q", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
}

func TestWebSearchCacheCorrupted(t *testing.T) {
	cache := &recordingCache{
		seedEntry: CacheEntry{Response: []byte("not-json")},
		seedHit:   true,
	}
	ddg := newDDGFake(t, []SourceHit{{URL: "https://corruption-fallback"}})
	defer ddg.Close()
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     ddg.URL(),
		HTTPClient: http.DefaultClient,
		Cache:      cache,
	})
	hits, err := w.Search(context.Background(), "q", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].URL != "https://corruption-fallback" {
		t.Fatalf("expected fallback hit, got %v", hits)
	}
}

func TestSearchWithExtractionNoFirecrawlReturnsRaw(t *testing.T) {
	ddg := newDDGFake(t, []SourceHit{{URL: "https://x", Excerpt: "raw"}})
	defer ddg.Close()
	w := NewWebSearch(WebSearchOptions{
		DDGURL:     ddg.URL(),
		HTTPClient: http.DefaultClient,
	})
	hits, err := w.SearchWithExtraction(context.Background(), "q", 1)
	if err != nil {
		t.Fatal(err)
	}
	if hits[0].Excerpt != "raw" {
		t.Errorf("excerpt = %q, want raw", hits[0].Excerpt)
	}
}

func TestSearchWithExtractionFirecrawl(t *testing.T) {
	ddg := newDDGFake(t, []SourceHit{{URL: "https://x", Excerpt: "raw"}})
	defer ddg.Close()
	fire := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"markdown": "# enriched"})
	}))
	defer fire.Close()
	w := NewWebSearch(WebSearchOptions{
		DDGURL:       ddg.URL(),
		FirecrawlURL: fire.URL,
		HTTPClient:   http.DefaultClient,
	})
	hits, err := w.SearchWithExtraction(context.Background(), "q", 1)
	if err != nil {
		t.Fatal(err)
	}
	if hits[0].Excerpt != "# enriched" {
		t.Errorf("excerpt = %q, want enriched", hits[0].Excerpt)
	}
}

func TestSearchWithExtractionFirecrawlFails(t *testing.T) {
	ddg := newDDGFake(t, []SourceHit{{URL: "https://x", Excerpt: "raw"}})
	defer ddg.Close()
	fire := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer fire.Close()
	w := NewWebSearch(WebSearchOptions{
		DDGURL:       ddg.URL(),
		FirecrawlURL: fire.URL,
		HTTPClient:   http.DefaultClient,
	})
	hits, err := w.SearchWithExtraction(context.Background(), "q", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(hits[0].Excerpt, "firecrawl-failed") {
		t.Errorf("expected firecrawl-failed prefix, got %q", hits[0].Excerpt)
	}
}

func TestSnippetTruncates(t *testing.T) {
	body := make([]byte, 500)
	for i := range body {
		body[i] = 'x'
	}
	got := snippet(body)
	if len(got) != 203 {
		t.Errorf("len = %d, want 203 (200+...)", len(got))
	}
	if got2 := snippet([]byte("short")); got2 != "short" {
		t.Errorf("snippet of short = %q", got2)
	}
}

type recordingCache struct {
	gets, sets int
	seedHit    bool
	seedEntry  CacheEntry
}

func (c *recordingCache) Get(_ context.Context, _ string) (CacheEntry, bool, error) {
	c.gets++
	if c.seedHit {
		return c.seedEntry, true, nil
	}
	return CacheEntry{}, false, nil
}
func (c *recordingCache) Set(_ context.Context, _ string, _ CacheEntry, _ int64) error {
	c.sets++
	return nil
}

type ddgFake struct {
	*httptest.Server
	calls int
	hits  []SourceHit
}

func newDDGFake(_ *testing.T, hits []SourceHit) *ddgFake {
	f := &ddgFake{hits: hits}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": f.hits})
	}))
	return f
}

func (f *ddgFake) URL() string { return f.Server.URL }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || hasSubstr(s, sub))
}

func hasSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
