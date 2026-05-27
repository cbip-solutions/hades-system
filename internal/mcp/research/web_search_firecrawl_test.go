package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirecrawlErrorIsSanitised(t *testing.T) {
	ddg := newDDGFake(t, []SourceHit{
		{Source: "ddg", URL: "https://example.com/a", Title: "A"},
	})
	defer ddg.Close()

	fire := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer fire.Close()

	w := NewWebSearch(WebSearchOptions{
		DDGURL:       ddg.URL(),
		FirecrawlURL: fire.URL,
		HTTPClient:   http.DefaultClient,
	})
	hits, err := w.SearchWithExtraction(context.Background(), "x", 1)
	if err != nil {
		t.Fatalf("SearchWithExtraction: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
	excerpt := hits[0].Excerpt
	// The excerpt MUST start with the [firecrawl-failed:...] marker.
	if !strings.HasPrefix(excerpt, "[firecrawl-failed:") {
		t.Errorf("excerpt missing marker: %q", excerpt)
	}
	// The excerpt MUST be single-line — sanitiser must escape any \n.
	if strings.ContainsAny(excerpt, "\n\r") {
		t.Errorf("excerpt contains newlines (C-11 regression): %q", excerpt)
	}

	if len(excerpt) > 600 {
		t.Errorf("excerpt length %d exceeds 600 (sanitiser cap missing)", len(excerpt))
	}
}

func TestFirecrawlSanitiserCapsHugeError(t *testing.T) {
	huge := strings.Repeat("x\"y\nz", 1000)
	got := sanitizeErrForExcerpt(&staticErr{msg: huge})

	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("sanitised error contains raw newlines: %q", got)
	}

	if len(got) > 500 {
		t.Errorf("sanitised error length %d exceeds reasonable bound (200 cap + quoting overhead)", len(got))
	}
}

type staticErr struct{ msg string }

func (e *staticErr) Error() string { return e.msg }
