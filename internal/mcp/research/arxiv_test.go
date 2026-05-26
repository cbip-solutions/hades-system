package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleArxivAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2401.00001v1</id>
    <updated>2026-01-01T00:00:00Z</updated>
    <title>Test Paper Title</title>
    <summary>This is a test
abstract.</summary>
    <author><name>Alice</name></author>
    <author><name>Bob</name></author>
    <link href="http://arxiv.org/abs/2401.00001v1" rel="alternate" type="text/html"/>
    <link href="http://arxiv.org/pdf/2401.00001v1" rel="related" type="application/pdf"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2401.00002v2</id>
    <updated>2026-01-02T00:00:00Z</updated>
    <title>Second Paper</title>
    <summary>Another summary</summary>
    <link href="http://arxiv.org/abs/2401.00002v2" rel="alternate" type="text/html"/>
  </entry>
</feed>`

func TestArxivHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("search_query"); got != "transformers" {
			t.Errorf("search_query = %q", got)
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(sampleArxivAtom))
	}))
	defer srv.Close()
	cache := &recordingCache{}
	a := NewArxiv(ArxivOptions{
		BaseURL: srv.URL,
		Cache:   cache,
	})
	hits, err := a.Search(context.Background(), "transformers", 10, "relevance")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d", len(hits))
	}
	if !strings.Contains(hits[0].Title, "Test Paper") {
		t.Errorf("title = %q", hits[0].Title)
	}
	if !strings.Contains(hits[0].Excerpt, "test abstract") {
		t.Errorf("excerpt = %q", hits[0].Excerpt)
	}
	if hits[0].URL != "http://arxiv.org/abs/2401.00001v1" {
		t.Errorf("url = %q", hits[0].URL)
	}
	if hits[0].Source != "arxiv" {
		t.Errorf("source = %q", hits[0].Source)
	}
	if cache.gets != 1 || cache.sets != 1 {
		t.Errorf("cache gets=%d sets=%d", cache.gets, cache.sets)
	}
}

func TestArxivCacheHit(t *testing.T) {
	cache := &recordingCache{
		seedHit:   true,
		seedEntry: CacheEntry{Response: []byte(`[{"source":"arxiv","url":"https://x","title":"t"}]`)},
	}
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	defer srv.Close()
	a := NewArxiv(ArxivOptions{BaseURL: srv.URL, Cache: cache})
	hits, err := a.Search(context.Background(), "x", 5, "relevance")
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("network was called despite cache hit")
	}
	if len(hits) != 1 {
		t.Fatal("expected 1 cached hit")
	}
}

func TestArxivEmptyQuery(t *testing.T) {
	a := NewArxiv(ArxivOptions{})
	if _, err := a.Search(context.Background(), "", 5, "relevance"); err == nil {
		t.Fatal("expected empty-query error")
	}
}

func TestArxivBadSortBy(t *testing.T) {
	a := NewArxiv(ArxivOptions{})
	if _, err := a.Search(context.Background(), "q", 5, "no-such-sort"); err == nil {
		t.Fatal("expected invalid sort_by error")
	}
}

func TestArxivDefaultsZeroMaxToTen(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("max_results")
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	}))
	defer srv.Close()
	a := NewArxiv(ArxivOptions{BaseURL: srv.URL})
	if _, err := a.Search(context.Background(), "q", 0, "relevance"); err != nil {
		t.Fatal(err)
	}
	if got != "10" {
		t.Errorf("max_results = %q, want 10", got)
	}
}

func TestArxiv500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer srv.Close()
	a := NewArxiv(ArxivOptions{BaseURL: srv.URL})
	if _, err := a.Search(context.Background(), "q", 5, "relevance"); err == nil {
		t.Fatal("expected 500 error")
	}
}

func TestArxivBadXML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-xml"))
	}))
	defer srv.Close()
	a := NewArxiv(ArxivOptions{BaseURL: srv.URL})
	if _, err := a.Search(context.Background(), "q", 5, "relevance"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestArxivBadURL(t *testing.T) {
	a := NewArxiv(ArxivOptions{BaseURL: "://bad"})
	if _, err := a.Search(context.Background(), "q", 5, "relevance"); err == nil {
		t.Fatal("expected URL error")
	}
}

func TestArxivCacheCorrupted(t *testing.T) {
	cache := &recordingCache{
		seedHit:   true,
		seedEntry: CacheEntry{Response: []byte("not-json")},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleArxivAtom))
	}))
	defer srv.Close()
	a := NewArxiv(ArxivOptions{BaseURL: srv.URL, Cache: cache})
	hits, err := a.Search(context.Background(), "q", 5, "relevance")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Errorf("hits = %d, want 2 (fallback)", len(hits))
	}
}

func TestArxivLastUpdatedSort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleArxivAtom))
	}))
	defer srv.Close()
	a := NewArxiv(ArxivOptions{BaseURL: srv.URL})
	if _, err := a.Search(context.Background(), "q", 5, "lastUpdatedDate"); err != nil {
		t.Fatal(err)
	}
}

func TestArxivSubmittedSort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleArxivAtom))
	}))
	defer srv.Close()
	a := NewArxiv(ArxivOptions{BaseURL: srv.URL})
	if _, err := a.Search(context.Background(), "q", 5, "submittedDate"); err != nil {
		t.Fatal(err)
	}
}

func TestArxivCacheKeyStable(t *testing.T) {
	a := arxivCacheKey("q", 10, "relevance")
	b := arxivCacheKey("q", 10, "relevance")
	if a != b {
		t.Errorf("hash unstable: %s vs %s", a, b)
	}
	c := arxivCacheKey("q", 10, "lastUpdatedDate")
	if c == a {
		t.Errorf("hash collision across sort")
	}
}

func TestArxivNoCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleArxivAtom))
	}))
	defer srv.Close()
	a := NewArxiv(ArxivOptions{BaseURL: srv.URL})
	hits, err := a.Search(context.Background(), "q", 10, "relevance")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Errorf("hits = %d", len(hits))
	}
}

func TestCleanWhitespace(t *testing.T) {
	got := cleanWhitespace("a    b\tc\nd")
	if got != "a b c d" {
		t.Errorf("got = %q", got)
	}
	got2 := cleanWhitespace("   ")
	if got2 != "" {
		t.Errorf("expected empty, got %q", got2)
	}
}
