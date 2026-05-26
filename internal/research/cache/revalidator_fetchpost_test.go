//go:build cgo
// +build cgo

package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRevalidatorFetchPOST_BasicPOST(t *testing.T) {
	var receivedBody []byte
	var receivedCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		receivedBody, _ = io.ReadAll(r.Body)
		receivedCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer srv.Close()

	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	res, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{
		Body:    []byte(`{"prompt":"hello"}`),
		Headers: map[string]string{"Content-Type": "application/json"},
	})
	if err != nil {
		t.Fatalf("FetchPOST: %v", err)
	}
	if !bytes.Equal(receivedBody, []byte(`{"prompt":"hello"}`)) {
		t.Fatalf("server received wrong body: %s", receivedBody)
	}
	if receivedCT != "application/json" {
		t.Fatalf("Content-Type header dropped: %q", receivedCT)
	}
	if res.HTTPStatusCode != 200 {
		t.Fatalf("status: %d", res.HTTPStatusCode)
	}
	if res.CacheHit {
		t.Fatal("FetchPOST default mode MUST NOT CAS-hit")
	}
	if res.FromCAS {
		t.Fatal("FetchPOST default mode MUST NOT touch CAS")
	}
	if !bytes.Equal(res.Body, []byte(`{"result":"ok"}`)) {
		t.Fatalf("response body wrong: %q", res.Body)
	}
	if res.ContentSHA256 == "" {
		t.Fatal("ContentSHA256 missing on POST response")
	}
}

func TestRevalidatorFetchPOST_CacheByBodyHash(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"result":"computed"}`))
	}))
	defer srv.Close()

	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	body := []byte(`{"prompt":"compute me"}`)

	res1, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{
		Body:            body,
		Headers:         map[string]string{"Content-Type": "application/json"},
		CacheByBodyHash: true,
	})
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if res1.CacheHit {
		t.Fatal("call 1 must NOT be cache-hit")
	}
	if !res1.FromCAS {
		t.Fatal("call 1 must report FromCAS=true (write)")
	}
	if res1.ContentSHA256 == "" {
		t.Fatal("ContentSHA256 missing")
	}

	res2, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{
		Body:            body,
		Headers:         map[string]string{"Content-Type": "application/json"},
		CacheByBodyHash: true,
	})
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if !res2.CacheHit {
		t.Fatal("call 2 must be cache-hit (same body hash)")
	}
	if !res2.FromCAS {
		t.Fatal("call 2 must report FromCAS=true")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 HTTP call (call 2 should hit CAS), got %d", callCount)
	}
	if !bytes.Equal(res2.Body, []byte(`{"result":"computed"}`)) {
		t.Fatalf("cached body mismatch: %q", res2.Body)
	}

	bodyH := sha256.Sum256(body)
	wantHash := hex.EncodeToString(bodyH[:])
	if res2.ContentSHA256 != wantHash {
		t.Errorf("ContentSHA256 = %q; want %q (CAS key = sha256(body))",
			res2.ContentSHA256, wantHash)
	}
}

func TestRevalidatorFetchPOST_HeadersForwarded(t *testing.T) {
	var gotAuth, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom-Tag")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	_, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{
		Body: []byte(`{}`),
		Headers: map[string]string{
			"Authorization": "Bearer secret123",
			"X-Custom-Tag":  "phase-a-test",
		},
	})
	if err != nil {
		t.Fatalf("FetchPOST: %v", err)
	}
	if gotAuth != "Bearer secret123" {
		t.Fatalf("Authorization header lost: %q", gotAuth)
	}
	if gotCustom != "phase-a-test" {
		t.Fatalf("X-Custom-Tag header lost: %q", gotCustom)
	}
}

func TestRevalidatorFetchPOST_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	_, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{
		Body:    []byte(`{}`),
		Timeout: 100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRevalidatorFetchPOST_UserAgentDefault(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	_, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{Body: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if gotUA == "" || !bytes.Contains([]byte(gotUA), []byte("zen-swarm")) {
		t.Fatalf("default User-Agent missing: %q", gotUA)
	}
}

func TestRevalidatorFetchPOST_UserAgentOverride(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	_, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{
		Body:      []byte(`{}`),
		UserAgent: "zen-ollama-cr/0.14.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != "zen-ollama-cr/0.14.0" {
		t.Fatalf("custom User-Agent dropped: %q", gotUA)
	}
}

// TestRevalidatorFetchPOST_NilBodyGuard — body MUST be non-nil.
func TestRevalidatorFetchPOST_NilBodyGuard(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	_, err := rv.FetchPOST(context.Background(), "http://example.com", FetchPOSTOptions{Body: nil})
	if err == nil {
		t.Fatal("expected error for nil Body")
	}
}

// TestRevalidatorFetchPOST_EmptyURLGuard — URL MUST be non-empty.
func TestRevalidatorFetchPOST_EmptyURLGuard(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	_, err := rv.FetchPOST(context.Background(), "", FetchPOSTOptions{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestRevalidatorFetchPOST_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := rv.FetchPOST(ctx, srv.URL, FetchPOSTOptions{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected context-cancel error")
	}
}

func TestRevalidatorFetchPOST_CacheByBodyHashNoCAS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	rv := NewRevalidator(ValidateOpts{Client: srv.Client()})
	res, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{
		Body:            []byte(`{"q":"x"}`),
		CacheByBodyHash: true,
	})
	if err != nil {
		t.Fatalf("FetchPOST: %v", err)
	}
	if res.CacheHit {
		t.Error("CacheHit = true; want false (no CAS available)")
	}
	if res.FromCAS {
		t.Error("FromCAS = true; want false (no CAS available)")
	}
}

// TestRevalidatorFetchPOST_CacheByBodyHashNon2xx — non-2xx response with
// CacheByBodyHash=true MUST NOT be cached (only 2xx writes to CAS).
func TestRevalidatorFetchPOST_CacheByBodyHashNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()
	rv, _ := newFetchTestRevalidator(t, srv.Client(), nil)
	body := []byte(`{"q":"trigger-500"}`)
	res, err := rv.FetchPOST(context.Background(), srv.URL, FetchPOSTOptions{
		Body:            body,
		CacheByBodyHash: true,
	})
	if err != nil {
		t.Fatalf("FetchPOST: %v", err)
	}
	if res.HTTPStatusCode != 500 {
		t.Errorf("status = %d; want 500", res.HTTPStatusCode)
	}
	if res.FromCAS {
		t.Error("FromCAS = true; want false (500 must not write to CAS)")
	}

	bodyH := sha256.Sum256(body)
	bodyHHex := hex.EncodeToString(bodyH[:])
	if _, err := rv.cas.Read(bodyHHex, "json"); err == nil {
		t.Error("CAS unexpectedly contains the 500-response body")
	}
}

func TestRevalidatorFetchPOST_InvalidURL(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	_, err := rv.FetchPOST(context.Background(), "http://example.com/\x7f", FetchPOSTOptions{
		Body: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected build-request error for malformed URL")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("research_cache:")) {
		t.Errorf("err = %v; want \"research_cache:\" prefix", err)
	}
}

func TestRevalidatorFetchPOST_DNSFailure(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	_, err := rv.FetchPOST(context.Background(),
		"http://test.invalid/never-resolves", FetchPOSTOptions{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected DNS-failure error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("HTTP")) {
		t.Errorf("err = %v; want substring \"HTTP\"", err)
	}
}

func TestRevalidatorFetchPOST_WriteCacheErrCASUnset(t *testing.T) {
	rv := NewRevalidator(ValidateOpts{})
	err := rv.writeFetchPOSTCache("deadbeef", []byte("x"))
	if !errors.Is(err, ErrCASUnset) {
		t.Errorf("err = %v; want ErrCASUnset", err)
	}
}

func TestRevalidatorFetchPOST_WriteCacheDedup(t *testing.T) {
	rv, _ := newFetchTestRevalidator(t, http.DefaultClient, nil)
	const hashHex = "feedfacefeedface0000000000000000000000000000000000000000000000ff"
	body := []byte(`{"dedup":1}`)
	if err := rv.writeFetchPOSTCache(hashHex, body); err != nil {
		t.Fatalf("write 1: %v", err)
	}

	if err := rv.writeFetchPOSTCache(hashHex, body); err != nil {
		t.Errorf("write 2 (dedup): %v; want nil", err)
	}
}

func TestRevalidatorFetchPOST_WriteCacheMkdirFails(t *testing.T) {
	rv, cas := newFetchTestRevalidator(t, http.DefaultClient, nil)

	const hashHex = "ababababababababababababababababababababababababababababababab00"
	prefixDir := filepath.Join(cas.Root(), hashHex[:2])

	if err := os.WriteFile(prefixDir, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("plant blocker file: %v", err)
	}
	err := rv.writeFetchPOSTCache(hashHex, []byte(`{}`))
	if err == nil {
		t.Fatal("expected mkdir error; got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("mkdir")) {
		t.Errorf("err = %v; want substring \"mkdir\"", err)
	}
}

func TestRevalidatorFetchPOST_WriteCacheStaleTmp(t *testing.T) {
	rv, cas := newFetchTestRevalidator(t, http.DefaultClient, nil)
	const hashHex = "abadcafeabadcafe000000000000000000000000000000000000000000000aa1"
	dest := cas.Path(hashHex, "json")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(dest+".tmp", []byte("crashed-writer"), 0o600); err != nil {
		t.Fatalf("plant tmp: %v", err)
	}
	err := rv.writeFetchPOSTCache(hashHex, []byte("new"))
	if err == nil {
		t.Fatal("expected EXCL-collision error on stale .tmp; got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("open tmp")) {
		t.Errorf("err = %v; want substring \"open tmp\"", err)
	}
}

func TestRevalidatorFetchPOST_WriteCacheStaleTmpDestRaced(t *testing.T) {
	rv, cas := newFetchTestRevalidator(t, http.DefaultClient, nil)
	const hashHex = "abadcafeabadcafe000000000000000000000000000000000000000000000bb2"
	dest := cas.Path(hashHex, "json")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := rv.writeFetchPOSTCache(hashHex, []byte("cached")); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	if err := os.WriteFile(dest+".tmp", []byte("inflight"), 0o600); err != nil {
		t.Fatalf("plant tmp: %v", err)
	}

	if err := rv.writeFetchPOSTCache(hashHex, []byte("new")); err != nil {
		t.Errorf("write: %v; want nil (dedup short-circuit)", err)
	}
}

func TestRevalidatorRecordFetchMetadataMkdirFails(t *testing.T) {
	rv, cas := newFetchTestRevalidator(t, http.DefaultClient, nil)
	const u = "http://record.test/blocked"

	idxPath := rv.fetchIndexPath(u)
	prefixDir := filepath.Dir(idxPath)

	if err := os.MkdirAll(filepath.Dir(prefixDir), 0o700); err != nil {
		t.Fatalf("mkdir _url_index/: %v", err)
	}
	if err := os.WriteFile(prefixDir, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("plant blocker file: %v", err)
	}
	_ = cas
	err := rv.recordFetchMetadata(u, "abc", "html", "", time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected mkdir error; got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("mkdir")) {
		t.Errorf("err = %v; want substring \"mkdir\"", err)
	}
}
