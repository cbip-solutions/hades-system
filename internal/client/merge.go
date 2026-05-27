// SPDX-License-Identifier: MIT
// Package client — merge.go.
//
// MergeHTTPClient is the production HTTP client for the daemon's
// /v1/merge/* surface (8 routes registered by the F-4 daemon handler in
// internal/daemon/handlers/merge.go). It satisfies the MergeClient
// interface declared in merge_dto.go (this package), which the cli
// package re-exports via type alias so the F-2 source surface is
// preserved end-to-end.
//
// Naming the struct is *MergeHTTPClient* (not *MergeClient*) because
// the interface in this same package is already named MergeClient —
// a concrete type sharing the interface name would shadow it. Other
// transports (e.g. a future fake-keyed in-memory client for chaos
// tests) can also satisfy MergeClient without name collision.
//
// Wire types live in merge_dto.go alongside the interface; invariant
// boundary preserved (no internal/orchestrator/merge import here).
//
// # Routes
//
// GET /v1/merge/inspect?id=<generation|hash>
// POST /v1/merge/replay (body: {"session_id": "..."})
// GET /v1/merge/score-explain?outcome_id=<id>
// GET /v1/merge/baseline?session_id=<id>
// GET /v1/merge/cache/status
// POST /v1/merge/cache/clear
// GET /v1/merge/config
// GET /v1/merge/anomaly?since=<duration>
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type MergeHTTPClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewMergeClient constructs the production HTTP client. Caller passes
// a pre-configured *http.Client (UDS dialer transport in the daemon
// integration path; httptest.Server.Client() in tests) plus a base URL.
//
// Both arguments MUST be non-nil / non-empty in the production path —
// the constructor is permissive for test convenience but the methods
// below will surface transport errors verbatim if they aren't.
//
// Returns *MergeHTTPClient (a struct that satisfies MergeClient, the
// interface). Callers that want the interface type for testability
// can assign to a MergeClient variable directly.
func NewMergeClient(httpClient *http.Client, baseURL string) *MergeHTTPClient {
	return &MergeHTTPClient{httpClient: httpClient, baseURL: baseURL}
}

func (c *MergeHTTPClient) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if query != nil {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("merge client GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("merge client GET %s: %d %s", path, resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("merge client decode %s: %w", path, err)
	}
	return nil
}

func (c *MergeHTTPClient) post(ctx context.Context, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("merge client encode %s: %w", path, err)
		}
		rdr = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("merge client POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("merge client POST %s: %d %s", path, resp.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
		return fmt.Errorf("merge client decode %s: %w", path, err)
	}
	return nil
}

func (c *MergeHTTPClient) Inspect(ctx context.Context, idOrHash string) (*MergeInspectResult, error) {
	var r MergeInspectResult
	q := url.Values{"id": []string{idOrHash}}
	if err := c.get(ctx, "/v1/merge/inspect", q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *MergeHTTPClient) Replay(ctx context.Context, sessionID string) (*MergeReplayResult, error) {
	var r MergeReplayResult
	if err := c.post(ctx, "/v1/merge/replay", map[string]string{"session_id": sessionID}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *MergeHTTPClient) ScoreExplain(ctx context.Context, outcomeID string) (*MergeScoreExplainResult, error) {
	var r MergeScoreExplainResult
	q := url.Values{"outcome_id": []string{outcomeID}}
	if err := c.get(ctx, "/v1/merge/score-explain", q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *MergeHTTPClient) BaselineShow(ctx context.Context, sessionID string) (*MergeBaselineShowResult, error) {
	var r MergeBaselineShowResult
	q := url.Values{"session_id": []string{sessionID}}
	if err := c.get(ctx, "/v1/merge/baseline", q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *MergeHTTPClient) CacheStatus(ctx context.Context) (*MergeCacheStatusResult, error) {
	var r MergeCacheStatusResult
	if err := c.get(ctx, "/v1/merge/cache/status", nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *MergeHTTPClient) CacheClear(ctx context.Context) error {
	return c.post(ctx, "/v1/merge/cache/clear", nil, nil)
}

func (c *MergeHTTPClient) ConfigShow(ctx context.Context) (*MergeConfigShowResult, error) {
	var r MergeConfigShowResult
	if err := c.get(ctx, "/v1/merge/config", nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *MergeHTTPClient) AnomalyList(ctx context.Context, since string) (*MergeAnomalyListResult, error) {
	var r MergeAnomalyListResult
	q := url.Values{"since": []string{since}}
	if err := c.get(ctx, "/v1/merge/anomaly", q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

var _ MergeClient = (*MergeHTTPClient)(nil)
