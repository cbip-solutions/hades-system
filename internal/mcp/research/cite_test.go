package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCiteVerifierEmpty(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})
	out, err := v.Verify(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty, got %d", len(out))
	}
}

func TestCiteVerifierHTTPGood(t *testing.T) {
	statuses := []int{200, 201, 301, 302, 304, 399}
	for _, s := range statuses {
		s := s
		t.Run(http.StatusText(s), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(s)
			}))
			defer srv.Close()
			v := NewCiteVerifier(CiteVerifierOptions{HTTPClient: srv.Client()})
			out, err := v.Verify(context.Background(), []RawCitation{
				{SourceID: "src", URL: srv.URL, Title: "t"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != 1 {
				t.Fatalf("status %d should pass; got %d verified", s, len(out))
			}
			if out[0].HTTPStatus != s {
				t.Errorf("HTTPStatus = %d, want %d", out[0].HTTPStatus, s)
			}
		})
	}
}

func TestCiteVerifier4xxStrips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	v := NewCiteVerifier(CiteVerifierOptions{HTTPClient: srv.Client()})
	out, err := v.Verify(context.Background(), []RawCitation{{URL: srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("404 should strip; got %d", len(out))
	}
}

func TestCiteVerifier5xxStrips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	v := NewCiteVerifier(CiteVerifierOptions{HTTPClient: srv.Client()})
	out, _ := v.Verify(context.Background(), []RawCitation{{URL: srv.URL}})
	if len(out) != 0 {
		t.Errorf("5xx should strip; got %d", len(out))
	}
}

func TestCiteVerifierDNSNXDOMAIN(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})

	out, _ := v.Verify(context.Background(), []RawCitation{
		{URL: "https://nonexistent-zenswarm-test.invalid/page"},
	})
	if len(out) != 0 {
		t.Errorf("unresolvable host should strip; got %d", len(out))
	}
}

func TestCiteVerifierLocalSchemePass(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})
	out, err := v.Verify(context.Background(), []RawCitation{
		{URL: "file:///local/path", SourceID: "f"},
		{URL: "caronte://node/pkg/foo", SourceID: "g"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("local schemes should pass; got %d", len(out))
	}
}

func TestCiteVerifierUnknownScheme(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})
	out, _ := v.Verify(context.Background(), []RawCitation{
		{URL: "ftp://server/file"},
	})
	if len(out) != 0 {
		t.Errorf("unknown scheme should strip; got %d", len(out))
	}
}

func TestCiteVerifierInvalidURL(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})
	out, _ := v.Verify(context.Background(), []RawCitation{
		{URL: "://broken"},
	})
	if len(out) != 0 {
		t.Errorf("invalid URL should strip; got %d", len(out))
	}
}

func TestCiteVerifierEmptyURL(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})
	out, _ := v.Verify(context.Background(), []RawCitation{
		{URL: "", SourceID: "x"},
	})
	if len(out) != 0 {
		t.Errorf("empty URL should strip; got %d", len(out))
	}
}

func TestCiteVerifierConcurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	v := NewCiteVerifier(CiteVerifierOptions{
		HTTPClient:    srv.Client(),
		MaxConcurrent: 4,
	})
	raw := make([]RawCitation, 20)
	for i := range raw {
		raw[i] = RawCitation{SourceID: "s", URL: srv.URL, Title: "t"}
	}
	out, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 20 {
		t.Errorf("expected 20 verified, got %d", len(out))
	}
}

func TestCiteVerifierFormatBasic(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})
	verified := []VerifiedCitation{
		{SourceID: "s1", URL: "https://a", Title: "A"},
		{SourceID: "s2", URL: "https://b", Title: ""},
	}
	md, structured := v.Format(verified)
	if !strings.Contains(md, "[1] A — https://a") {
		t.Errorf("md missing first entry: %s", md)
	}
	if !strings.Contains(md, "[2] https://b — https://b") {
		t.Errorf("md missing second entry (title fallback): %s", md)
	}
	var parsed map[string]any
	if err := json.Unmarshal(structured, &parsed); err != nil {
		t.Fatalf("structured unmarshal: %v", err)
	}
	if _, ok := parsed["gen_ai.citations"]; !ok {
		t.Errorf("structured missing gen_ai.citations key: %s", structured)
	}
}

func TestCiteVerifierFormatEmpty(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})
	md, structured := v.Format(nil)
	if md != "" {
		t.Errorf("md = %q, want empty", md)
	}
	if string(structured) != "[]" {
		t.Errorf("structured = %s, want []", structured)
	}
}

func TestCiteVerifierMaxConcurrentDefault(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{})
	if v.opts.MaxConcurrent != 8 {
		t.Errorf("default MaxConcurrent = %d", v.opts.MaxConcurrent)
	}
}

func TestCiteVerifierLocalSchemesOverride(t *testing.T) {
	v := NewCiteVerifier(CiteVerifierOptions{
		LocalSchemes: []string{"custom"},
	})
	out, _ := v.Verify(context.Background(), []RawCitation{
		{URL: "custom://anything"},
		{URL: "file:///nope"},
	})
	if len(out) != 1 {
		t.Errorf("expected only custom-scheme verified, got %d", len(out))
	}
}
