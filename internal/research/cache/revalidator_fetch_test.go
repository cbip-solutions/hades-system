//go:build cgo
// +build cgo

package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newFetchTestRevalidator(t *testing.T, client *http.Client, urlToTTL map[string]time.Duration) (*Revalidator, *CAS) {
	t.Helper()
	dir := t.TempDir()
	cas, err := NewCAS(filepath.Join(dir, "cas"))
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	rv := NewRevalidator(ValidateOpts{
		Client:  client,
		Timeout: 2 * time.Second,
		CAS:     cas,
		TTLLookup: func(u string) time.Duration {
			if d, ok := urlToTTL[u]; ok {
				return d
			}
			return 24 * time.Hour
		},
	})
	return rv, cas
}

func TestRevalidatorFetchCacheHitFresh(t *testing.T) {
	const body = "fresh cache body"
	hash := sha256.Sum256([]byte(body))
	hashHex := hex.EncodeToString(hash[:])

	httpCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	url := srv.URL + "/doc.html"
	rv, cas := newFetchTestRevalidator(t, srv.Client(), map[string]time.Duration{
		url: 1 * time.Hour,
	})

	if _, err := cas.Write([]byte(body), "html"); err != nil {
		t.Fatalf("CAS.Write: %v", err)
	}
	if err := rv.recordFetchMetadata(url, hashHex, "html", "test-etag", time.Now(), time.Now()); err != nil {
		t.Fatalf("recordFetchMetadata: %v", err)
	}

	res, err := rv.Fetch(context.Background(), url, FetchOptions{AcceptETag: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.CacheHit {
		t.Errorf("CacheHit = false; want true (TTL fresh path)")
	}
	if !res.FromCAS {
		t.Errorf("FromCAS = false; want true")
	}
	if res.HTTPStatusCode != 0 {
		t.Errorf("HTTPStatusCode = %d; want 0 (no HTTP issued)", res.HTTPStatusCode)
	}
	if string(res.Body) != body {
		t.Errorf("Body = %q; want %q", string(res.Body), body)
	}
	if res.ContentSHA256 != hashHex {
		t.Errorf("ContentSHA256 = %q; want %q", res.ContentSHA256, hashHex)
	}
	if httpCalls != 0 {
		t.Errorf("httpCalls = %d; want 0 (CAS hit path should never issue HTTP)", httpCalls)
	}
}

func TestRevalidatorFetchCacheMissHTTPGet(t *testing.T) {
	const body = "fresh GET body content"
	etag := "\"v1-abc\""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET; got %s", r.Method)
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	url := srv.URL + "/missing.html"
	rv, _ := newFetchTestRevalidator(t, srv.Client(), map[string]time.Duration{
		url: 1 * time.Hour,
	})

	res, err := rv.Fetch(context.Background(), url, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.CacheHit {
		t.Errorf("CacheHit = true; want false (cache miss path)")
	}
	if !res.FromCAS {
		t.Errorf("FromCAS = false; want true (Fetch must write fresh body to CAS)")
	}
	if res.HTTPStatusCode != http.StatusOK {
		t.Errorf("HTTPStatusCode = %d; want 200", res.HTTPStatusCode)
	}
	if string(res.Body) != body {
		t.Errorf("Body = %q; want %q", string(res.Body), body)
	}
	hash := sha256.Sum256([]byte(body))
	wantHash := hex.EncodeToString(hash[:])
	if res.ContentSHA256 != wantHash {
		t.Errorf("ContentSHA256 = %q; want %q", res.ContentSHA256, wantHash)
	}
	if res.ETag != etag {
		t.Errorf("ETag = %q; want %q", res.ETag, etag)
	}
}

func TestRevalidatorFetch304NotModified(t *testing.T) {
	const cachedBody = "the cached body content"
	cachedHash := sha256.Sum256([]byte(cachedBody))
	cachedHashHex := hex.EncodeToString(cachedHash[:])
	cachedETag := "\"v1-abc\""

	getCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET; got %s", r.Method)
			return
		}
		if got := r.Header.Get("If-None-Match"); got != cachedETag {
			t.Errorf("If-None-Match = %q; want %q", got, cachedETag)
		}
		getCalls++
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	url := srv.URL + "/maybe-changed.html"
	rv, cas := newFetchTestRevalidator(t, srv.Client(), map[string]time.Duration{
		url: 1 * time.Millisecond,
	})
	if _, err := cas.Write([]byte(cachedBody), "html"); err != nil {
		t.Fatalf("CAS.Write seed: %v", err)
	}

	pastTime := time.Now().Add(-1 * time.Hour)
	if err := rv.recordFetchMetadata(url, cachedHashHex, "html", cachedETag, pastTime, pastTime); err != nil {
		t.Fatalf("recordFetchMetadata: %v", err)
	}

	res, err := rv.Fetch(context.Background(), url, FetchOptions{AcceptETag: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.CacheHit {
		t.Errorf("CacheHit = true; want false (revalidate path; HTTP was issued)")
	}
	if !res.FromCAS {
		t.Errorf("FromCAS = false; want true (read from CAS on 304)")
	}
	if res.HTTPStatusCode != http.StatusNotModified {
		t.Errorf("HTTPStatusCode = %d; want %d (304)", res.HTTPStatusCode, http.StatusNotModified)
	}
	if string(res.Body) != cachedBody {
		t.Errorf("Body = %q; want %q (304 must return cached body)", string(res.Body), cachedBody)
	}
	if res.ContentSHA256 != cachedHashHex {
		t.Errorf("ContentSHA256 = %q; want %q (304 must preserve cached SHA)",
			res.ContentSHA256, cachedHashHex)
	}
	if getCalls != 1 {
		t.Errorf("getCalls = %d; want 1 (single conditional GET expected)", getCalls)
	}
}

func TestRevalidatorFetchMismatchRefetch(t *testing.T) {
	const cachedBody = "old body"
	const freshBody = "completely different new body"
	cachedHash := sha256.Sum256([]byte(cachedBody))
	cachedHashHex := hex.EncodeToString(cachedHash[:])
	freshHash := sha256.Sum256([]byte(freshBody))
	freshHashHex := hex.EncodeToString(freshHash[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "\"v2-xyz\"")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, freshBody)
	}))
	defer srv.Close()

	url := srv.URL + "/changed.html"
	rv, cas := newFetchTestRevalidator(t, srv.Client(), map[string]time.Duration{
		url: 1 * time.Millisecond,
	})
	if _, err := cas.Write([]byte(cachedBody), "html"); err != nil {
		t.Fatalf("CAS.Write seed: %v", err)
	}
	pastTime := time.Now().Add(-1 * time.Hour)
	if err := rv.recordFetchMetadata(url, cachedHashHex, "html", "\"v1-abc\"", pastTime, pastTime); err != nil {
		t.Fatalf("recordFetchMetadata: %v", err)
	}

	res, err := rv.Fetch(context.Background(), url, FetchOptions{AcceptETag: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(res.Body) != freshBody {
		t.Errorf("Body = %q; want %q (mismatch must return new body)", string(res.Body), freshBody)
	}
	if res.ContentSHA256 != freshHashHex {
		t.Errorf("ContentSHA256 = %q; want %q (mismatch must update SHA)",
			res.ContentSHA256, freshHashHex)
	}
	if res.HTTPStatusCode != http.StatusOK {
		t.Errorf("HTTPStatusCode = %d; want 200 (mismatch path is GET+200)", res.HTTPStatusCode)
	}

	gotHash, err := rv.lookupURLHash(url)
	if err != nil {
		t.Fatalf("lookupURLHash: %v", err)
	}
	if gotHash != freshHashHex {
		t.Errorf("URL-index hash = %q; want %q (updated to new content)",
			gotHash, freshHashHex)
	}
}

func TestRevalidatorFetchForceRefresh(t *testing.T) {
	const body = "force-refreshed body"
	getCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getCalls++
		if got := r.Header.Get("If-None-Match"); got != "" {
			t.Errorf("If-None-Match = %q; want empty (ForceRefresh strips conditional)", got)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	url := srv.URL + "/forced.html"
	rv, cas := newFetchTestRevalidator(t, srv.Client(), map[string]time.Duration{
		url: 24 * time.Hour,
	})

	prev := []byte("old")
	prevHash := sha256.Sum256(prev)
	if _, err := cas.Write(prev, "html"); err != nil {
		t.Fatalf("CAS.Write seed: %v", err)
	}
	if err := rv.recordFetchMetadata(url, hex.EncodeToString(prevHash[:]), "html",
		"\"v1\"", time.Now(), time.Now()); err != nil {
		t.Fatalf("recordFetchMetadata: %v", err)
	}

	res, err := rv.Fetch(context.Background(), url, FetchOptions{
		ForceRefresh: true,
		AcceptETag:   true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.CacheHit {
		t.Errorf("CacheHit = true; want false (ForceRefresh path)")
	}
	if string(res.Body) != body {
		t.Errorf("Body = %q; want %q", string(res.Body), body)
	}
	if getCalls != 1 {
		t.Errorf("getCalls = %d; want 1", getCalls)
	}
}

func TestRevalidatorFetchTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	url := srv.URL + "/slow.html"
	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)

	_, err := rv.Fetch(context.Background(), url, FetchOptions{Timeout: 100 * time.Millisecond})
	if err == nil {
		t.Fatalf("Fetch: expected error; got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {

		if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "timeout") {
			t.Errorf("Fetch error = %v; want context.DeadlineExceeded or timeout wrap", err)
		}
	}
}

func TestRevalidatorFetchDNSFailure(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)

	_, err := rv.Fetch(context.Background(),
		"http://test.invalid/does-not-resolve", FetchOptions{})
	if err == nil {
		t.Fatalf("Fetch: expected error; got nil")
	}

	if !strings.Contains(err.Error(), "research_cache:") {
		t.Errorf("Fetch error = %v; want wrapped with package prefix", err)
	}
}

func TestRevalidatorFetchContextCancel(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-released
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(released)

	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := rv.Fetch(ctx, srv.URL+"/long.html", FetchOptions{Timeout: 5 * time.Second})
	if err == nil {
		t.Fatalf("Fetch: expected error; got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Fetch error = %v; want context.Canceled", err)
	}
}

func TestRevalidatorFetchEmptyURL(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	_, err := rv.Fetch(context.Background(), "", FetchOptions{})
	if !errors.Is(err, ErrSourceURLRequired) {
		t.Errorf("Fetch(\"\") error = %v; want ErrSourceURLRequired", err)
	}
}

func TestRevalidatorFetchUserAgent(t *testing.T) {
	gotUA := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "x")
	}))
	defer srv.Close()
	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)

	if _, err := rv.Fetch(context.Background(), srv.URL+"/x.html",
		FetchOptions{UserAgent: "test-agent/1.0"}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotUA != "test-agent/1.0" {
		t.Errorf("UA = %q; want %q", gotUA, "test-agent/1.0")
	}

	if _, err := rv.Fetch(context.Background(), srv.URL+"/y.html", FetchOptions{}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.HasPrefix(gotUA, "zen-swarm/") {
		t.Errorf("default UA = %q; want prefix \"zen-swarm/\"", gotUA)
	}
}

func TestRevalidatorFetchPersistsETag(t *testing.T) {
	const body = "etag-test"
	const wantETag = "\"abc123\""
	requestNum := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNum++
		if requestNum == 1 {
			w.Header().Set("ETag", wantETag)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, body)
			return
		}

		if got := r.Header.Get("If-None-Match"); got != wantETag {
			t.Errorf("2nd call If-None-Match = %q; want %q", got, wantETag)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	url := srv.URL + "/cached.html"
	rv, _ := newFetchTestRevalidator(t, srv.Client(), map[string]time.Duration{
		url: 1 * time.Millisecond,
	})

	first, err := rv.Fetch(context.Background(), url, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch #1: %v", err)
	}
	if first.ETag != wantETag {
		t.Errorf("first ETag = %q; want %q", first.ETag, wantETag)
	}

	time.Sleep(10 * time.Millisecond)

	second, err := rv.Fetch(context.Background(), url, FetchOptions{AcceptETag: true})
	if err != nil {
		t.Fatalf("Fetch #2: %v", err)
	}
	if second.HTTPStatusCode != http.StatusNotModified {
		t.Errorf("Fetch #2 HTTPStatusCode = %d; want 304", second.HTTPStatusCode)
	}
	if string(second.Body) != body {
		t.Errorf("Fetch #2 body = %q; want %q (304 returns cached body)",
			string(second.Body), body)
	}
}
