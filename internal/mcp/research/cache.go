// SPDX-License-Identifier: MIT
// cache.go — research cache adapter.
//
// Wraps the daemon /v1/research/cache/{get,set} endpoints (
// typed client) into the CacheClient interface declared in types.go.
//
// Cache key derivation: sha256(query + sorted_sources + iteration_index)
// so callers compute the deterministic key via CacheKey() helper.
package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type CacheAdapterOptions struct {
	DaemonURL string

	AuthToken string

	HTTPClient *http.Client
}

type CacheAdapter struct {
	opts CacheAdapterOptions
}

func NewCacheAdapter(opts CacheAdapterOptions) *CacheAdapter {
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	return &CacheAdapter{opts: opts}
}

var _ CacheClient = (*CacheAdapter)(nil)

func CacheKey(query string, sources []string, iteration int) string {
	sorted := make([]string, len(sources))
	copy(sorted, sources)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(query))
	h.Write([]byte("|"))
	h.Write([]byte(strings.Join(sorted, ",")))
	h.Write([]byte("|"))
	h.Write(fmt.Appendf(nil, "%d", iteration))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *CacheAdapter) Get(ctx context.Context, hash string) (CacheEntry, bool, error) {
	if c.opts.DaemonURL == "" {
		return CacheEntry{}, false, nil
	}
	url := strings.TrimRight(c.opts.DaemonURL, "/") + "/v1/research/cache/get?hash=" + hash
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CacheEntry{}, false, err
	}
	if c.opts.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.opts.AuthToken)
	}
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return CacheEntry{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return CacheEntry{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return CacheEntry{}, false, fmt.Errorf("cache get: status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)

	return CacheEntry{Hash: hash, Response: body}, true, nil
}

func (c *CacheAdapter) Set(ctx context.Context, hash string, entry CacheEntry, ttlSecs int64) error {
	if c.opts.DaemonURL == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"hash":     hash,
		"response": json.RawMessage(entry.Response),
		"ttl_secs": ttlSecs,
	})
	if err != nil {
		return err
	}
	url := strings.TrimRight(c.opts.DaemonURL, "/") + "/v1/research/cache/set"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.opts.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.opts.AuthToken)
	}
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cache set: status %d", resp.StatusCode)
	}
	return nil
}
