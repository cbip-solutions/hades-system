// SPDX-License-Identifier: MIT
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type CacheHit struct {
	Response string `json:"response"`

	Hash string `json:"hash"`

	ExpiresAt time.Time `json:"expires_at"`
}

type CacheSetRequest struct {
	Query string

	Hash string

	Response string

	TTL time.Duration
}

func hashForRequest(req CacheSetRequest) string {
	if req.Hash != "" {
		return req.Hash
	}
	sum := sha256.Sum256([]byte(req.Query))
	return fmt.Sprintf("%x", sum)
}

func hashForQuery(query string) string {
	sum := sha256.Sum256([]byte(query))
	return fmt.Sprintf("%x", sum)
}

// CacheClient wraps *Client to provide typed access to the daemon
// /v1/research/cache/{get,set} endpoints.
//
// Concurrency safe for concurrent use after construction. CacheClient
// holds only an immutable *Client reference; Get and Set are stateless
// at this layer (the daemon is the source of truth) so concurrent
// callers do not contend on any in-process state.
type CacheClient struct {
	c *Client
}

func NewCacheClient(c *Client) *CacheClient {
	return &CacheClient{c: c}
}

func (cc *CacheClient) Get(ctx context.Context, query string) (*CacheHit, error) {
	hash := hashForQuery(query)
	url := cc.c.BaseURL() + "/v1/research/cache/get?hash=" + hash

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cache.Get: build request: %w", err)
	}

	resp, err := cc.c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cache.Get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {

		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cache.Get: daemon returned %d", resp.StatusCode)
	}

	var hit CacheHit
	if err := json.NewDecoder(resp.Body).Decode(&hit); err != nil {
		return nil, fmt.Errorf("cache.Get: decode response: %w", err)
	}

	if hit.Hash == "" {
		hit.Hash = hash
	}
	return &hit, nil
}

func (cc *CacheClient) Set(ctx context.Context, req CacheSetRequest) error {
	if req.TTL == 0 {
		req.TTL = 7 * 24 * time.Hour
	}
	hash := hashForRequest(req)

	body := struct {
		Hash     string `json:"hash"`
		Response string `json:"response"`
		TTLNS    int64  `json:"ttl_ns"`
	}{
		Hash:     hash,
		Response: req.Response,
		TTLNS:    req.TTL.Nanoseconds(),
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cache.Set: marshal: %w", err)
	}

	url := cc.c.BaseURL() + "/v1/research/cache/set"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("cache.Set: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	resp, err := cc.c.Do(httpReq)
	if err != nil {
		return fmt.Errorf("cache.Set: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cache.Set: daemon returned %d", resp.StatusCode)
	}
	return nil
}
