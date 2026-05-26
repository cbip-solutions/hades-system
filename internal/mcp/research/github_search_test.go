package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleGitHubSearchJSON = `{
  "total_count": 2,
  "incomplete_results": false,
  "items": [
    {
      "id": 1,
      "name": "tokenizers",
      "full_name": "huggingface/tokenizers",
      "html_url": "https://github.com/huggingface/tokenizers",
      "description": "Fast state-of-the-art tokenizers",
      "stargazers_count": 9000,
      "language": "Rust"
    },
    {
      "id": 2,
      "name": "transformers",
      "full_name": "huggingface/transformers",
      "html_url": "https://github.com/huggingface/transformers",
      "description": "State-of-the-art ML",
      "stargazers_count": 130000,
      "language": "Python"
    }
  ]
}`

func newGitHubFake(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleGitHubSearchJSON))
	}
	mux.HandleFunc("/search/repositories", handler)
	mux.HandleFunc("/api/v3/search/repositories", handler)
	srv := httptest.NewServer(mux)
	return srv
}

func TestGitHubSearchHappyPath(t *testing.T) {
	srv := newGitHubFake(t)
	defer srv.Close()
	cache := &recordingCache{}
	g := NewGitHubSearch(GitHubSearchOptions{
		BaseURL:    srv.URL + "/",
		HTTPClient: http.DefaultClient,
		Cache:      cache,
	})
	hits, err := g.Search(context.Background(), "ml frameworks", "Python", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].Source != "github" {
		t.Errorf("source = %q", hits[0].Source)
	}
	if !strings.Contains(hits[0].URL, "github.com") {
		t.Errorf("url = %q", hits[0].URL)
	}
	if cache.gets != 1 || cache.sets != 1 {
		t.Errorf("cache gets=%d sets=%d", cache.gets, cache.sets)
	}
}

func TestGitHubSearchCacheHit(t *testing.T) {
	cache := &recordingCache{
		seedHit:   true,
		seedEntry: CacheEntry{Response: []byte(`[{"source":"github","url":"https://x","title":"t"}]`)},
	}
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	defer srv.Close()
	g := NewGitHubSearch(GitHubSearchOptions{
		BaseURL: srv.URL + "/",
		Cache:   cache,
	})
	hits, err := g.Search(context.Background(), "x", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("API called despite cache hit")
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
}

func TestGitHubSearchEmptyQuery(t *testing.T) {
	g := NewGitHubSearch(GitHubSearchOptions{})
	if _, err := g.Search(context.Background(), "", "", 0); err == nil {
		t.Fatal("expected error on empty query")
	}
}

func TestGitHubSearchAuthToken(t *testing.T) {
	gotAuth := ""
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}
	mux.HandleFunc("/api/v3/search/repositories", handler)
	mux.HandleFunc("/search/repositories", handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	g := NewGitHubSearch(GitHubSearchOptions{
		BaseURL:   srv.URL + "/",
		AuthToken: "ghp_testtoken",
	})
	if _, err := g.Search(context.Background(), "q", "", 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotAuth, "ghp_testtoken") {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestGitHubSearchAPIError(t *testing.T) {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"rate limit exceeded"}`, http.StatusForbidden)
	}
	mux.HandleFunc("/search/repositories", handler)
	mux.HandleFunc("/api/v3/search/repositories", handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	g := NewGitHubSearch(GitHubSearchOptions{BaseURL: srv.URL + "/"})
	if _, err := g.Search(context.Background(), "q", "", 0); err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestGitHubSearchCacheKeyStable(t *testing.T) {
	a := githubCacheKey("q", "Go", 100)
	b := githubCacheKey("q", "Go", 100)
	if a != b {
		t.Errorf("hash unstable")
	}
	c := githubCacheKey("q", "Go", 101)
	if c == a {
		t.Errorf("hash collision across stars_min")
	}
}

func TestGitHubSearchQueryDSL(t *testing.T) {
	gotQ := ""
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}
	mux.HandleFunc("/search/repositories", handler)
	mux.HandleFunc("/api/v3/search/repositories", handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	g := NewGitHubSearch(GitHubSearchOptions{BaseURL: srv.URL + "/"})
	if _, err := g.Search(context.Background(), "tokenizers", "Rust", 100); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQ, "language:Rust") {
		t.Errorf("q = %q, missing language qualifier", gotQ)
	}
	if !strings.Contains(gotQ, "stars:>=100") {
		t.Errorf("q = %q, missing stars qualifier", gotQ)
	}
}

func TestGitHubSearchNilDescription(t *testing.T) {
	body := `{"items":[{"id":1,"full_name":"foo/bar","html_url":"https://x"}]}`
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
	mux.HandleFunc("/search/repositories", handler)
	mux.HandleFunc("/api/v3/search/repositories", handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	g := NewGitHubSearch(GitHubSearchOptions{BaseURL: srv.URL + "/"})
	hits, err := g.Search(context.Background(), "q", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
	if hits[0].Excerpt != "" {
		t.Errorf("expected empty excerpt, got %q", hits[0].Excerpt)
	}
}

func TestGitHubSearchCacheCorruptedFallsThrough(t *testing.T) {
	srv := newGitHubFake(t)
	defer srv.Close()
	cache := &recordingCache{
		seedHit:   true,
		seedEntry: CacheEntry{Response: []byte("not-json")},
	}
	g := NewGitHubSearch(GitHubSearchOptions{
		BaseURL: srv.URL + "/",
		Cache:   cache,
	})
	hits, err := g.Search(context.Background(), "q", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Errorf("expected fallback (2), got %d", len(hits))
	}
}

func TestGitHubSearchBadBaseURL(t *testing.T) {
	g := NewGitHubSearch(GitHubSearchOptions{BaseURL: "://no"})
	if g == nil {
		t.Fatal("constructor returned nil")
	}

}

func TestDeref(t *testing.T) {
	if got := deref(nil); got != "" {
		t.Errorf("got %q", got)
	}
	s := "hi"
	if got := deref(&s); got != "hi" {
		t.Errorf("got %q", got)
	}
}
