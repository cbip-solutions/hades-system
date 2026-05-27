//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package cache — revalidator.go
//
// This file is the SOLE legal HTTP callsite in internal/research/cache/.
// The noWebInCache AST analyzer enforces this
// boundary via an allowlist that names this file specifically. All other
// files in this package MUST NOT import net/http; the analyzer treats any
// other file-level net/http import as a violation of invariant.
//
// # Revalidator
//
// Revalidator implements the HEAD-based revalidation protocol per spec §7.4
// (T5 mitigation). Given a Finding with a SourceURL and ContentHash, it:
//
// 1. Issues a HEAD request to SourceURL with If-None-Match + If-Modified-Since
// conditional headers (if available from the Finding).
// 2. Interprets the response status:
// - 304 Not Modified → FreshnessFresh (content unchanged).
// - 200 OK → fetchAndCompare (GET + sha256 vs ContentHash).
// - 404 / 410 → FreshnessStale, nil error (source gone; demote).
// - 5xx → error (caller retries per failure mode #11 §7.1).
// - other → error (unexpected response).
// 3. fetchAndCompare GETs the final URL (after any redirects followed by
// stdlib http.Client), reads the body, computes sha256, and compares to
// finding.ContentHash: match → FreshnessFresh; mismatch → FreshnessStale.
//
// Redirects are followed automatically by http.Client (stdlib default;
// up to 10 hops). ValidateResult.FinalURL is set to the final URL after
// redirect resolution.
//
// Context cancellation is respected at all phases: the per-request context
// carries both the caller's deadline and an additional per-request timeout
// (ValidateOpts.Timeout, default 5 s). The tighter of the two wins per
// context.WithTimeout semantics.
//
// invariant: SourceURL must be non-empty. Validate returns
// ErrSourceURLRequired when finding.URL is empty.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const defaultRevalidatorTimeout = 5 * time.Second

var ErrSourceURLRequired = errors.New("research_cache: finding.URL is required for revalidation (inv-hades-152)")

type ValidateOpts struct {
	Client *http.Client

	Timeout time.Duration

	CAS *CAS

	TTLLookup func(url string) time.Duration
}

type ValidateResult struct {
	Status FreshnessStatus

	NewContentHash string

	FinalURL string

	ETag string

	LastModified string
}

type Revalidator struct {
	client  *http.Client
	timeout time.Duration

	cas       *CAS
	ttlLookup func(url string) time.Duration

	indexMu  sync.RWMutex
	urlIndex map[string]fetchIndexEntry
}

func NewRevalidator(opts ValidateOpts) *Revalidator {
	client := opts.Client
	if client == nil {
		client = &http.Client{}
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultRevalidatorTimeout
	}
	ttlFn := opts.TTLLookup
	if ttlFn == nil {
		ttlFn = LookupTTL
	}
	rv := &Revalidator{
		client:    client,
		timeout:   timeout,
		cas:       opts.CAS,
		ttlLookup: ttlFn,
		urlIndex:  make(map[string]fetchIndexEntry),
	}

	if opts.CAS != nil {
		_ = rv.loadFetchMetadata()
	}
	return rv
}

func (rv *Revalidator) Validate(ctx context.Context, finding Finding) (*ValidateResult, error) {
	if finding.URL == "" {
		return nil, ErrSourceURLRequired
	}

	reqCtx, cancel := context.WithTimeout(ctx, rv.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, finding.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("research_cache: revalidator: build HEAD request: %w", err)
	}
	if finding.ContentHash != "" {

		req.Header.Set("If-None-Match", finding.ContentHash)
	}

	resp, err := rv.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("research_cache: revalidator: HEAD %s: %w", finding.URL, err)
	}
	defer resp.Body.Close()

	finalURL := finding.URL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	etag := resp.Header.Get("ETag")
	lastModified := resp.Header.Get("Last-Modified")

	switch resp.StatusCode {
	case http.StatusNotModified:
		return &ValidateResult{
			Status:       FreshnessFresh,
			FinalURL:     finalURL,
			ETag:         etag,
			LastModified: lastModified,
		}, nil

	case http.StatusNotFound, http.StatusGone:
		return &ValidateResult{
			Status:   FreshnessStale,
			FinalURL: finalURL,
		}, nil

	case http.StatusOK:
		return rv.fetchAndCompare(reqCtx, finding, finalURL)

	default:
		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("research_cache: revalidator: HEAD %s returned %s", finding.URL, resp.Status)
		}
		return nil, fmt.Errorf("research_cache: revalidator: HEAD %s returned unexpected status %s", finding.URL, resp.Status)
	}
}

func (rv *Revalidator) fetchAndCompare(ctx context.Context, finding Finding, finalURL string) (*ValidateResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, finalURL, nil)
	if err != nil {
		return nil, fmt.Errorf("research_cache: revalidator: build GET request: %w", err)
	}

	resp, err := rv.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("research_cache: revalidator: GET %s: %w", finalURL, err)
	}
	defer resp.Body.Close()

	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("research_cache: revalidator: read body from %s: %w", finalURL, err)
	}

	sum := sha256.Sum256(body)
	newHash := hex.EncodeToString(sum[:])

	etag := resp.Header.Get("ETag")
	lastModified := resp.Header.Get("Last-Modified")

	var status FreshnessStatus
	if newHash == finding.ContentHash {
		status = FreshnessFresh
	} else {
		status = FreshnessStale
	}

	return &ValidateResult{
		Status:         status,
		NewContentHash: newHash,
		FinalURL:       finalURL,
		ETag:           etag,
		LastModified:   lastModified,
	}, nil
}
