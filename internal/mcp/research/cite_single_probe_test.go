// cite_single_probe_test.go — regression test for C-1 (CodeReview Plan 4
// Phase I): the cite verifier MUST issue exactly ONE HTTP HEAD per
// citation URL. Prior implementation called passes() (1 probe) then
// re-called probeStatus() (2nd probe) to capture the status code; this
// doubled outbound HEAD traffic + made rate-limit hits twice as likely.
package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCiteSingleProbe_HappyPath(t *testing.T) {
	var count int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&count, 1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	v := NewCiteVerifier(CiteVerifierOptions{HTTPClient: srv.Client()})
	out, err := v.Verify(context.Background(), []RawCitation{{URL: srv.URL, SourceID: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("verified = %d, want 1", len(out))
	}
	got := atomic.LoadInt64(&count)
	if got != 1 {
		t.Errorf("HEAD probe count = %d, want exactly 1 (C-1: no double probe)", got)
	}
	if out[0].HTTPStatus != 200 {
		t.Errorf("HTTPStatus = %d, want 200 (status from the single probe)", out[0].HTTPStatus)
	}
}

func TestCiteSingleProbe_StripPath(t *testing.T) {
	var count int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&count, 1)
		w.WriteHeader(404)
	}))
	defer srv.Close()
	v := NewCiteVerifier(CiteVerifierOptions{HTTPClient: srv.Client()})
	out, _ := v.Verify(context.Background(), []RawCitation{{URL: srv.URL}})
	if len(out) != 0 {
		t.Errorf("404 should strip; got %d", len(out))
	}
	got := atomic.LoadInt64(&count)
	if got != 1 {
		t.Errorf("HEAD probe count = %d, want exactly 1 (C-1: no double probe)", got)
	}
}

func TestCiteSingleProbe_BulkConcurrent(t *testing.T) {
	var count int64
	var perURL sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&count, 1)
		v, _ := perURL.LoadOrStore(r.URL.Path, new(int64))
		atomic.AddInt64(v.(*int64), 1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	v := NewCiteVerifier(CiteVerifierOptions{
		HTTPClient:    srv.Client(),
		MaxConcurrent: 4,
	})
	const N = 10
	raw := make([]RawCitation, N)
	for i := 0; i < N; i++ {
		raw[i] = RawCitation{URL: srv.URL + "/p" + string(rune('0'+i))}
	}
	out, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != N {
		t.Errorf("verified = %d, want %d", len(out), N)
	}
	got := atomic.LoadInt64(&count)
	if got != int64(N) {
		t.Errorf("total HEAD count = %d, want %d (C-1: exactly one probe per URL)", got, N)
	}

	perURL.Range(func(_, val any) bool {
		c := atomic.LoadInt64(val.(*int64))
		if c != 1 {
			t.Errorf("per-URL count = %d, want 1", c)
		}
		return true
	})
}

func TestCiteSingleProbe_LocalSchemeNoProbe(t *testing.T) {
	var count int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&count, 1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	v := NewCiteVerifier(CiteVerifierOptions{HTTPClient: srv.Client()})
	out, _ := v.Verify(context.Background(), []RawCitation{
		{URL: "caronte://node/x"},
		{URL: "file:///local"},
	})
	if len(out) != 2 {
		t.Errorf("verified = %d, want 2 (local schemes pass)", len(out))
	}
	if got := atomic.LoadInt64(&count); got != 0 {
		t.Errorf("HEAD count = %d, want 0 (local schemes do NOT probe)", got)
	}
}
