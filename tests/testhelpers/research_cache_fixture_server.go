// SPDX-License-Identifier: MIT
// Package testhelpers — research_cache_fixture_server.go
//
// ResearchCacheFixtureServer is an httptest.Server with deterministic,
// semantically-named paths used by the research-cache revalidator tests
// .
//
// Each path exercises one response scenario:
//
// /fresh-etag — HEAD returns 304 (If-None-Match matches); used for
// TestRevalidator304Fresh + TestRevalidatorRedirectFollowed.
// /changed-etag — HEAD returns 200 with a new body; GET returns the
// new body (different from the fixture hash). Used for
// TestRevalidator200ContentMismatch.
// /timeout — sleeps 6 s then returns 200; used for
// TestRevalidatorTimeoutRespected (Revalidator.timeout=200ms).
// /500 — always returns 500; used for
// TestRevalidator500RetriesThenFails.
// /redirect — returns 301 → /fresh-etag; used for
// TestRevalidatorRedirectFollowed.
// /404 — always returns 404; used for
// TestRevalidator404DemotesStale.
// /poisoned — returns 200 HEAD but a different body on GET;
// reserved for future adversarial tests (inv-zen-T9).
//
// Sha256Hex is a convenience wrapper around crypto/sha256 returning the
// lowercase hexadecimal digest of data. Used by revalidator_test.go to
// build Finding.ContentHash values that are consistent with what
// fetchAndCompare produces.
package testhelpers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

const FixedETag = `"zen-f7-fixture-abc123"`

const FreshBody = "fresh fixture body — zen-swarm Plan 9 F-7"

const ChangedBody = "changed fixture body — different from stored hash"

type ResearchCacheFixtureServer struct {
	Server *http.Server

	URL string

	inner *httptest.Server
}

// NewResearchCacheFixtureServer starts an httptest.Server with the
// deterministic paths and returns a ResearchCacheFixtureServer.
//
// The caller MUST call Close (or register t.Cleanup(srv.Close)) when
// done.
//
// Pre: none.
// Post: srv.inner is started; srv.URL is the base URL.
func NewResearchCacheFixtureServer() *ResearchCacheFixtureServer {
	mux := http.NewServeMux()
	srv := &ResearchCacheFixtureServer{}

	mux.HandleFunc("/fresh-etag", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", FixedETag)
		w.Header().Set("Last-Modified", "Sat, 01 Jan 2000 00:00:00 GMT")
		if r.Method == http.MethodHead {
			ifNoneMatch := r.Header.Get("If-None-Match")
			if ifNoneMatch == FixedETag {
				w.WriteHeader(http.StatusNotModified)
				return
			}

			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(FreshBody)))
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(FreshBody))
	})

	const changedETag = `"zen-f7-changed-xyz789"`
	mux.HandleFunc("/changed-etag", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", changedETag)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method == http.MethodHead {

			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ChangedBody))
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

		http.Redirect(w, r, srv.URL+"/fresh-etag", http.StatusMovedPermanently)
	})

	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	mux.HandleFunc("/poisoned", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", FixedETag)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method == http.MethodHead {

			w.WriteHeader(http.StatusOK)
			return
		}

		_, _ = w.Write([]byte("tampered body — sha256 intentionally mismatches stored hash"))
	})

	inner := httptest.NewServer(mux)
	srv.inner = inner
	srv.Server = inner.Config
	srv.URL = inner.URL

	return srv
}

func (s *ResearchCacheFixtureServer) Close() {
	s.inner.Close()
}

func Sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
