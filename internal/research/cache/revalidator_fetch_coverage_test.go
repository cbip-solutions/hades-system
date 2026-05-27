// go:build cgo
//go:build cgo
// +build cgo

// Package cache — revalidator_fetch_coverage_test.go
//
// Coverage extension for revalidator_fetch.go.
//
// The base suite (revalidator_fetch_test.go) covers the happy paths +
// the 11 documented behavioural scenarios. This file extends coverage
// over the error branches + edge paths that must reach ≥90% per the
// security/correctness-critical floor (HTTP boundary).
//
// Branches covered here:
// - ErrCASUnset — NewRevalidator without CAS → Fetch error
// - 404 / 410 source-gone responses — wrapped error with status
// - 5xx server error responses — wrapped error
// - 4xx non-404/410 (unexpected status) — wrapped error
// - http.NewRequestWithContext failure — invalid URL parse
// - 304 without prior CAS entry (protocol) — wrapped error
// - guessFetchExt all Content-Type branches — html/plain/markdown/xml/json/binary
// - guessFetchExt URL-suffix fallback —.pdf,.png etc.
// - lookupURLHash disk-fallback path — entry on disk but not in memory
// - lookupURLHash not-found error — neither memory nor disk
// - loadFetchMetadata round-trip persistence — write, re-construct, observe
// - loadFetchMetadata corrupted-json error — non-JSON file in _url_index
// - loadFetchMetadata _url_index is a file — not a directory
// - AcceptModSince adds If-Modified-Since — exercises the header set
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRevalidatorFetchErrCASUnset(t *testing.T) {
	rv := NewRevalidator(ValidateOpts{})
	_, err := rv.Fetch(context.Background(), "http://example.invalid/", FetchOptions{})
	if !errors.Is(err, ErrCASUnset) {
		t.Errorf("Fetch err = %v; want ErrCASUnset", err)
	}
}

func TestRevalidatorFetch404Gone(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"404", http.StatusNotFound},
		{"410", http.StatusGone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
			_, err := rv.Fetch(context.Background(), srv.URL+"/missing", FetchOptions{})
			if err == nil {
				t.Fatalf("Fetch: expected error for status %d; got nil", tc.status)
			}
			if !strings.Contains(err.Error(), "source gone") {
				t.Errorf("err = %v; want substring \"source gone\"", err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("status %d", tc.status)) {
				t.Errorf("err = %v; want status %d in message", err, tc.status)
			}
		})
	}
}

func TestRevalidatorFetch5xxServerError(t *testing.T) {
	cases := []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable}
	for _, status := range cases {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()
			rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
			_, err := rv.Fetch(context.Background(), srv.URL+"/boom", FetchOptions{})
			if err == nil {
				t.Fatalf("Fetch: expected error for status %d; got nil", status)
			}
			if !strings.Contains(err.Error(), "server error") {
				t.Errorf("err = %v; want substring \"server error\"", err)
			}
		})
	}
}

func TestRevalidatorFetchUnexpectedStatus(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	_, err := rv.Fetch(context.Background(), srv.URL+"/teapot", FetchOptions{})
	if err == nil {
		t.Fatal("Fetch: expected error for 418; got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 418") {
		t.Errorf("err = %v; want substring \"unexpected status 418\"", err)
	}
}

func TestRevalidatorFetchInvalidURL(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)

	_, err := rv.Fetch(context.Background(), "http://example.com/\x7f", FetchOptions{})
	if err == nil {
		t.Fatal("Fetch: expected error for malformed URL; got nil")
	}
	if !strings.Contains(err.Error(), "research_cache:") {
		t.Errorf("err = %v; want \"research_cache:\" prefix", err)
	}
}

func TestRevalidatorFetch304WithoutPriorCAS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	_, err := rv.Fetch(context.Background(), srv.URL+"/spooky", FetchOptions{})
	if err == nil {
		t.Fatal("Fetch: expected error for 304 without prior entry; got nil")
	}
	if !strings.Contains(err.Error(), "304 without prior CAS entry") {
		t.Errorf("err = %v; want substring \"304 without prior CAS entry\"", err)
	}
}

func TestGuessFetchExtBranches(t *testing.T) {
	makeResp := func(ct string) *http.Response {
		return &http.Response{Header: http.Header{"Content-Type": []string{ct}}}
	}
	cases := []struct {
		name string
		ct   string
		url  string
		want string
	}{
		{"json", "application/json", "http://x.test/foo", "json"},
		{"html", "text/html; charset=utf-8", "http://x.test/foo", "html"},
		{"plain", "text/plain", "http://x.test/foo", "txt"},
		{"markdown", "text/markdown", "http://x.test/foo", "txt"},
		{"xml-application", "application/xml", "http://x.test/foo", "xml"},
		{"xml-text", "text/xml", "http://x.test/foo", "xml"},
		{"suffix-pdf", "", "http://x.test/spec.pdf", "pdf"},
		{"suffix-png", "", "http://x.test/img.PNG", "png"},
		{"bin-default", "application/octet-stream", "http://x.test/anonymous", "bin"},
		{"bin-no-suffix-no-ct", "", "http://x.test/whatever", "bin"},
		{"long-suffix-ignored", "", "http://x.test/file.verylongext", "bin"},
		{"bad-url-falls-to-bin", "", "://not-a-url", "bin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := guessFetchExt(makeResp(tc.ct), tc.url)
			if got != tc.want {
				t.Errorf("guessFetchExt(%q, %q) = %q; want %q", tc.ct, tc.url, got, tc.want)
			}
		})
	}
}

func TestRevalidatorLookupURLHashDiskFallback(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	const url = "http://memhole.test/document"
	const wantHash = "deadbeef00000000000000000000000000000000000000000000000000000000"

	idxPath := rv.fetchIndexPath(url)
	if err := os.MkdirAll(filepath.Dir(idxPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const payload = `{"url":"http://memhole.test/document","hash":"deadbeef00000000000000000000000000000000000000000000000000000000","ext":"html","fetched_at":"2026-05-17T00:00:00Z"}`
	if err := os.WriteFile(idxPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := rv.lookupURLHash(url)
	if err != nil {
		t.Fatalf("lookupURLHash: %v", err)
	}
	if got != wantHash {
		t.Errorf("lookupURLHash = %q; want %q", got, wantHash)
	}
}

func TestRevalidatorLookupURLHashNotFound(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	_, err := rv.lookupURLHash("http://ghost.test/never-fetched")
	if err == nil {
		t.Fatal("lookupURLHash: expected error for unknown URL; got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v; want substring \"not found\"", err)
	}
}

func TestRevalidatorLookupURLHashCorruptDisk(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	const url = "http://corrupt.test/bad-json"
	idxPath := rv.fetchIndexPath(url)
	if err := os.MkdirAll(filepath.Dir(idxPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(idxPath, []byte("{not-json}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := rv.lookupURLHash(url)
	if err == nil {
		t.Fatal("lookupURLHash: expected unmarshal error; got nil")
	}
}

func TestRevalidatorLoadFetchMetadataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cas, err := NewCAS(filepath.Join(dir, "cas"))
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	rv1 := NewRevalidator(ValidateOpts{CAS: cas})

	const u = "http://persist.test/doc"
	const h = "abadcafe00000000000000000000000000000000000000000000000000000000"
	when := time.Now().UTC().Truncate(time.Second)
	if err := rv1.recordFetchMetadata(u, h, "html", "\"etag1\"", when, when); err != nil {
		t.Fatalf("recordFetchMetadata: %v", err)
	}

	rv2 := NewRevalidator(ValidateOpts{CAS: cas})
	entry, ok := rv2.lookupIndexEntry(u)
	if !ok {
		t.Fatal("rv2.lookupIndexEntry: not found after round-trip; loadFetchMetadata missed entry")
	}
	if entry.Hash != h {
		t.Errorf("entry.Hash = %q; want %q", entry.Hash, h)
	}
	if entry.ETag != "\"etag1\"" {
		t.Errorf("entry.ETag = %q; want \"etag1\"", entry.ETag)
	}
}

func TestRevalidatorLoadFetchMetadataCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	cas, err := NewCAS(filepath.Join(dir, "cas"))
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	rv := NewRevalidator(ValidateOpts{CAS: cas})

	idxDir := filepath.Join(cas.Root(), "_url_index", "ff")
	if err := os.MkdirAll(idxDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	corruptPath := filepath.Join(idxDir, "ff00000000000000000000000000000000000000000000000000000000000000.json")
	if err := os.WriteFile(corruptPath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err = rv.loadFetchMetadata()
	if err == nil {
		t.Fatal("loadFetchMetadata: expected error for corrupt JSON; got nil")
	}
	if !strings.Contains(err.Error(), "load url-index") {
		t.Errorf("err = %v; want substring \"load url-index\"", err)
	}
}

func TestRevalidatorLoadFetchMetadataIndexIsFile(t *testing.T) {
	dir := t.TempDir()
	cas, err := NewCAS(filepath.Join(dir, "cas"))
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}

	if err := os.WriteFile(filepath.Join(cas.Root(), "_url_index"), []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rv := NewRevalidator(ValidateOpts{CAS: cas})
	err = rv.loadFetchMetadata()
	if err == nil {
		t.Fatal("loadFetchMetadata: expected error for file-where-dir-should-be; got nil")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("err = %v; want substring \"not a directory\"", err)
	}
}

func TestRevalidatorFetchAcceptModSince(t *testing.T) {
	const body = "modtime-test"
	requestNum := 0
	var gotIMS string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNum++
		if requestNum == 1 {
			w.Header().Set("Last-Modified", "Sat, 01 Jan 2000 00:00:00 GMT")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, body)
			return
		}
		gotIMS = r.Header.Get("If-Modified-Since")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	url := srv.URL + "/modcache.html"
	rv, _ := newFetchTestRevalidator(t, srv.Client(), map[string]time.Duration{
		url: 1 * time.Millisecond,
	})

	if _, err := rv.Fetch(context.Background(), url, FetchOptions{}); err != nil {
		t.Fatalf("Fetch #1: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := rv.Fetch(context.Background(), url, FetchOptions{AcceptModSince: true}); err != nil {
		t.Fatalf("Fetch #2: %v", err)
	}
	if gotIMS != "Sat, 01 Jan 2000 00:00:00 GMT" {
		t.Errorf("If-Modified-Since = %q; want \"Sat, 01 Jan 2000 00:00:00 GMT\"", gotIMS)
	}
}

func TestRevalidatorFetchCacheFreshBlobMissing(t *testing.T) {
	const body = "refetched"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	url := srv.URL + "/orphan.html"
	rv, cas := newFetchTestRevalidator(t, srv.Client(), map[string]time.Duration{
		url: 1 * time.Hour,
	})

	phantomHash := sha256.Sum256([]byte("never-stored"))
	phantomHex := hex.EncodeToString(phantomHash[:])
	if err := rv.recordFetchMetadata(url, phantomHex, "html", "", time.Now(), time.Now()); err != nil {
		t.Fatalf("recordFetchMetadata: %v", err)
	}

	if _, err := cas.Read(phantomHex, "html"); !errors.Is(err, ErrBlobMissing) {
		t.Fatalf("setup: phantom blob unexpectedly present (err=%v)", err)
	}

	res, err := rv.Fetch(context.Background(), url, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.CacheHit {
		t.Errorf("CacheHit = true; want false (blob missing → must re-fetch)")
	}
	if string(res.Body) != body {
		t.Errorf("Body = %q; want %q", string(res.Body), body)
	}
	if res.HTTPStatusCode != http.StatusOK {
		t.Errorf("HTTPStatusCode = %d; want 200", res.HTTPStatusCode)
	}
}

func TestRevalidatorRecordFetchMetadataErrCASUnset(t *testing.T) {
	rv := NewRevalidator(ValidateOpts{})
	err := rv.recordFetchMetadata("http://x.test/", "abc", "html", "", time.Now(), time.Now())
	if !errors.Is(err, ErrCASUnset) {
		t.Errorf("err = %v; want ErrCASUnset", err)
	}
}

func TestRevalidatorLookupURLHashCASUnset(t *testing.T) {
	rv := NewRevalidator(ValidateOpts{})
	_, err := rv.lookupURLHash("http://x.test/orphan")
	if err == nil {
		t.Fatal("lookupURLHash: expected error when cas unset; got nil")
	}
	if !strings.Contains(err.Error(), "cas unset") {
		t.Errorf("err = %v; want substring \"cas unset\"", err)
	}
}

func TestRevalidatorRecordFetchMetadataRenameFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-onto-non-empty-dir behaviour is POSIX-specific")
	}
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)

	const url = "http://x.test/rename-victim"
	idxPath := rv.fetchIndexPath(url)

	if err := os.MkdirAll(filepath.Dir(idxPath), 0o700); err != nil {
		t.Fatalf("mkdir prefix: %v", err)
	}

	if err := os.MkdirAll(idxPath, 0o700); err != nil {
		t.Fatalf("mkdir idxPath-as-dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(idxPath, "blocker.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}

	err := rv.recordFetchMetadata(url, "abc", "html", "etag", time.Now(), time.Now())
	if err == nil {
		t.Fatal("recordFetchMetadata: expected rename error; got nil")
	}
	if !strings.Contains(err.Error(), "url-index rename:") {
		t.Errorf("err = %v; want substring \"url-index rename:\"", err)
	}

	tmpPath := idxPath + ".tmp"
	if _, statErr := os.Stat(tmpPath); statErr == nil {
		t.Errorf("stale .tmp left on disk after rename failure: %s", tmpPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("unexpected stat error on tmpPath: %v", statErr)
	}
}

func TestRevalidatorLoadFetchMetadataWalkError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission denial is POSIX-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0 does not deny access")
	}
	dir := t.TempDir()
	cas, err := NewCAS(filepath.Join(dir, "cas"))
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	rv := NewRevalidator(ValidateOpts{CAS: cas})

	prefixDir := filepath.Join(cas.Root(), "_url_index", "ab")
	if err := os.MkdirAll(prefixDir, 0o700); err != nil {
		t.Fatalf("mkdir prefix: %v", err)
	}
	jsonPath := filepath.Join(prefixDir, "abcd"+strings.Repeat("0", 60)+".json")
	if err := os.WriteFile(jsonPath, []byte(`{"url":"x","hash":"y","ext":"html","fetched_at":"2026-05-17T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("seed json: %v", err)
	}

	if err := os.Chmod(prefixDir, 0); err != nil {
		t.Fatalf("chmod 0 prefix: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(prefixDir, 0o700) })

	err = rv.loadFetchMetadata()
	if err == nil {
		t.Fatal("loadFetchMetadata: expected Walk error; got nil")
	}

	if err.Error() == "" {
		t.Errorf("err message empty; want OS-level Walk error")
	}
}

func TestRevalidatorLoadFetchMetadataReadFileError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission denial is POSIX-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0 does not deny access")
	}
	dir := t.TempDir()
	cas, err := NewCAS(filepath.Join(dir, "cas"))
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	rv := NewRevalidator(ValidateOpts{CAS: cas})

	prefixDir := filepath.Join(cas.Root(), "_url_index", "cd")
	if err := os.MkdirAll(prefixDir, 0o700); err != nil {
		t.Fatalf("mkdir prefix: %v", err)
	}
	jsonPath := filepath.Join(prefixDir, "cdef"+strings.Repeat("0", 60)+".json")
	if err := os.WriteFile(jsonPath, []byte(`{"url":"x","hash":"y","ext":"html","fetched_at":"2026-05-17T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("seed json: %v", err)
	}

	if err := os.Chmod(jsonPath, 0); err != nil {
		t.Fatalf("chmod 0 jsonPath: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(jsonPath, 0o600) })

	err = rv.loadFetchMetadata()
	if err == nil {
		t.Fatal("loadFetchMetadata: expected ReadFile error; got nil")
	}

	if err.Error() == "" {
		t.Errorf("err message empty; want OS-level ReadFile error")
	}
}
