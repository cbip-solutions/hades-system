// go:build cgo
//go:build cgo
// +build cgo

package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testFixedETag = `"zen-f7-fixture-abc123"`

const testFreshBody = "fresh fixture body — zen-swarm Plan 9 F-7"

const testChangedBody = "changed fixture body — different from stored hash"

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type revalFixtureServer struct {
	srv *httptest.Server
	URL string
}

func newRevalFixtureServer() *revalFixtureServer {
	var s revalFixtureServer
	mux := http.NewServeMux()

	mux.HandleFunc("/fresh-etag", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", testFixedETag)
		w.Header().Set("Last-Modified", "Sat, 01 Jan 2000 00:00:00 GMT")
		if r.Method == http.MethodHead {
			if r.Header.Get("If-None-Match") == testFixedETag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Length", "42")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testFreshBody))
	})

	mux.HandleFunc("/changed-etag", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"zen-f7-changed-xyz789"`)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testChangedBody))
	})

	mux.HandleFunc("/timeout", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(6 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("too late"))
	})

	mux.HandleFunc("/500", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})

	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.URL+"/fresh-etag", http.StatusMovedPermanently)
	})

	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	mux.HandleFunc("/poisoned", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", testFixedETag)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte("tampered body — sha256 intentionally mismatches"))
	})

	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	return &s
}

func (s *revalFixtureServer) Close() { s.srv.Close() }

func makeFinding(srv *revalFixtureServer, path, contentHash string) Finding {
	return Finding{
		ID:          "test-finding-id",
		DispatchID:  "test-dispatch-id",
		URL:         srv.URL + path,
		Title:       "test title",
		Snippet:     "test snippet",
		Freshness:   FreshnessUnknown,
		RetrievedAt: time.Now().Add(-1 * time.Hour).Unix(),
		ContentHash: contentHash,
	}
}

func TestRevalidator304Fresh(t *testing.T) {
	t.Parallel()
	srv := newRevalFixtureServer()
	defer srv.Close()

	r := NewRevalidator(ValidateOpts{})
	finding := makeFinding(srv, "/fresh-etag", testFixedETag)

	result, err := r.Validate(context.Background(), finding)
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if result.Status != FreshnessFresh {
		t.Errorf("want FreshnessFresh, got %v", result.Status)
	}
}

func TestRevalidator200ContentMismatch(t *testing.T) {
	t.Parallel()
	srv := newRevalFixtureServer()
	defer srv.Close()

	r := NewRevalidator(ValidateOpts{})

	finding := makeFinding(srv, "/changed-etag", sha256Hex([]byte(testFreshBody)))

	result, err := r.Validate(context.Background(), finding)
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if result.Status != FreshnessStale {
		t.Errorf("want FreshnessStale, got %v", result.Status)
	}
}

func TestRevalidator500RetriesThenFails(t *testing.T) {
	t.Parallel()
	srv := newRevalFixtureServer()
	defer srv.Close()

	r := NewRevalidator(ValidateOpts{})
	finding := makeFinding(srv, "/500", sha256Hex([]byte(testFreshBody)))

	_, err := r.Validate(context.Background(), finding)
	if err == nil {
		t.Fatal("Validate: want error on 5xx, got nil")
	}
}

func TestRevalidatorTimeoutRespected(t *testing.T) {
	t.Parallel()
	srv := newRevalFixtureServer()
	defer srv.Close()

	r := NewRevalidator(ValidateOpts{Timeout: 200 * time.Millisecond})
	finding := makeFinding(srv, "/timeout", sha256Hex([]byte(testFreshBody)))

	start := time.Now()
	_, err := r.Validate(context.Background(), finding)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Validate: want timeout error, got nil")
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Validate took %v — timeout was not respected (want <2s)", elapsed)
	}
}

func TestRevalidatorRedirectFollowed(t *testing.T) {
	t.Parallel()
	srv := newRevalFixtureServer()
	defer srv.Close()

	r := NewRevalidator(ValidateOpts{})

	finding := makeFinding(srv, "/redirect", sha256Hex([]byte(testFreshBody)))

	result, err := r.Validate(context.Background(), finding)
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if !strings.HasSuffix(result.FinalURL, "/fresh-etag") {
		t.Errorf("want FinalURL ending /fresh-etag, got %q", result.FinalURL)
	}
	if result.Status != FreshnessFresh {
		t.Errorf("want FreshnessFresh after redirect, got %v", result.Status)
	}
}

func TestRevalidator404DemotesStale(t *testing.T) {
	t.Parallel()
	srv := newRevalFixtureServer()
	defer srv.Close()

	r := NewRevalidator(ValidateOpts{})
	finding := makeFinding(srv, "/404", sha256Hex([]byte(testFreshBody)))

	result, err := r.Validate(context.Background(), finding)
	if err != nil {
		t.Fatalf("Validate: unexpected error on 404: %v", err)
	}
	if result.Status != FreshnessStale {
		t.Errorf("want FreshnessStale on 404, got %v", result.Status)
	}
}

func TestRevalidatorContextCancel(t *testing.T) {
	t.Parallel()
	srv := newRevalFixtureServer()
	defer srv.Close()

	r := NewRevalidator(ValidateOpts{})
	finding := makeFinding(srv, "/fresh-etag", testFixedETag)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Validate(ctx, finding)
	if err == nil {
		t.Fatal("Validate: want error on cancelled context, got nil")
	}
}

func TestRevalidatorEmptyURLReturnsError(t *testing.T) {
	t.Parallel()

	r := NewRevalidator(ValidateOpts{})
	finding := Finding{
		ID:          "test-empty-url",
		DispatchID:  "test-dispatch-id",
		URL:         "",
		ContentHash: sha256Hex([]byte(testFreshBody)),
	}

	_, err := r.Validate(context.Background(), finding)
	if err == nil {
		t.Fatal("Validate: want ErrSourceURLRequired, got nil")
	}
	if err != ErrSourceURLRequired {
		t.Errorf("want ErrSourceURLRequired, got %v", err)
	}
}
