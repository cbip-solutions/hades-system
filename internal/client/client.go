// SPDX-License-Identifier: MIT
// Package client is the typed HTTP client for talking to zen-swarm-ctld
// over its UDS socket. Used by the zen CLI and TUI.
//
// Phase L Task L-2 extends this surface with /v1/bypass/* helpers in
// bypass.go and a base-URL constructor for tests.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type DebugLogger interface {
	Logf(format string, args ...any)
}

type Client struct {
	udsPath string
	baseURL string
	httpC   *http.Client
	debug   DebugLogger
}

func (c *Client) SetDebugLogger(d DebugLogger) {
	c.debug = d
}

type HealthResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

func New(udsPath string) *Client {
	return &Client{
		udsPath: udsPath,
		httpC: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", udsPath)
				},
			},
			Timeout: 30 * time.Second,
		},
	}
}

func NewWithBaseURL(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpC:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) urlFor(path string) string {
	if c.baseURL != "" {
		return c.baseURL + path
	}
	return "http://unix" + path
}

// HTTPClient returns the underlying *http.Client. Exposed so callers
// that compose helper transports (e.g. MergeHTTPClient via
// NewMergeClient) reuse the same UDS-dialer transport instead of
// constructing a duplicate. Returned client carries the dialer + 30s
// timeout configured at New / NewWithBaseURL time.
//
// The returned reference is shared, not a copy — callers MUST NOT
// mutate Transport / Timeout. Treat as read-only.
func (c *Client) HTTPClient() *http.Client { return c.httpC }

func (c *Client) BaseURL() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "http://unix"
}

type HTTPError struct {
	Method  string
	Path    string
	Status  int
	RawBody []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, string(e.RawBody))
}

func IsHTTPStatus(err error, status int) bool {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status == status
	}
	return false
}

func (c *Client) debugLog(method, path string, status int, latency time.Duration, errStr string) {
	if c.debug == nil {
		return
	}
	if errStr != "" {
		c.debug.Logf("%s %s -> ERR=%s (%s)\n", method, path, errStr, latency)
		return
	}
	c.debug.Logf("%s %s -> %d (%s)\n", method, path, status, latency)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.getJSONH(ctx, path, nil, out)
}

// getJSONH executes GET with optional per-call headers and decodes the
// response into out. Pass nil for headers when no extra headers are
// required (getJSON is the nil-headers wrapper).
//
// send `X-Zen-Project-ID: <alias>` so the daemon mcpgateway dispatcher
// can resolve the alias canonically (header) instead of pulling it
// from request body args (which is the fallback per Phase A).
//
// Per-call header map MUST NOT be mutated after the call returns (the
// implementation iterates it once on a copy of the request header set).
func (c *Client) getJSONH(ctx context.Context, path string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor(path), nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := c.httpC.Do(req)
	if err != nil {
		c.debugLog(http.MethodGet, path, 0, time.Since(start), err.Error())
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	c.debugLog(http.MethodGet, path, resp.StatusCode, time.Since(start), "")
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		he := &HTTPError{Method: http.MethodGet, Path: path, Status: resp.StatusCode, RawBody: body}
		return fmt.Errorf("GET %s: %d %s: %w", path, resp.StatusCode, string(body), he)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	return c.postJSONH(ctx, path, nil, body, out)
}

// postJSONH executes POST with optional per-call headers and decodes
// the response into out (out may be nil). Pass nil for headers when no
// extra headers are required (postJSON is the nil-headers wrapper).
//
// `X-Zen-Project-ID: <alias>` through this surface so the daemon
// mcpgateway dispatcher resolves the alias canonically per the MCP
// protocol convention (the body-args path is Phase A's fallback only).
//
// Header semantics:
//   - The Content-Type header is set to application/json automatically
//     whenever body is non-nil (preserved from postJSON).
//   - Per-call headers from the headers map are set AFTER Content-Type
//     so a deliberate Content-Type override (rare) is honored.
//   - The headers map MUST NOT be mutated after the call returns.
func (c *Client) postJSONH(ctx context.Context, path string, headers map[string]string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = readerOf(buf)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlFor(path), rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := c.httpC.Do(req)
	if err != nil {
		c.debugLog(http.MethodPost, path, 0, time.Since(start), err.Error())
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	c.debugLog(http.MethodPost, path, resp.StatusCode, time.Since(start), "")
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		he := &HTTPError{Method: http.MethodPost, Path: path, Status: resp.StatusCode, RawBody: bodyBytes}
		return fmt.Errorf("POST %s: %d %s: %w", path, resp.StatusCode, string(bodyBytes), he)
	}
	if out == nil {
		return nil
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func readerOf(buf []byte) io.Reader { return &byteReader{b: buf} }

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.getJSON(ctx, "/v1/health", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
