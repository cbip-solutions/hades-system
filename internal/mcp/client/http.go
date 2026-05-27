// SPDX-License-Identifier: MIT
package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var ErrHostNotAllowed = errors.New("mcp/client: outbound host not in allowedHosts whitelist (inv-hades-085)")

const defaultSocketPath = "/var/run/hades-system/hades-system.sock"

const defaultAuthTokenPath = "~/.config/hades-system/auth-token"

const retryMaxAttempts = 3

const retryBaseDelay = 100 * time.Millisecond

const retryMaxDelay = 800 * time.Millisecond

type Config struct {
	SocketPath string

	BaseURL string

	AuthTokenPath string

	AllowedHosts []string

	MCPName string
}

type Client struct {
	cfg        Config
	token      string
	httpClient *http.Client
	allowed    map[string]struct{}
}

func New(cfg Config) (*Client, error) {
	if cfg.SocketPath == "" && cfg.BaseURL == "" {
		cfg.SocketPath = defaultSocketPath
	}
	if cfg.AuthTokenPath == "" {
		cfg.AuthTokenPath = expandHome(defaultAuthTokenPath)
	} else {
		cfg.AuthTokenPath = expandHome(cfg.AuthTokenPath)
	}

	info, err := os.Stat(cfg.AuthTokenPath)
	if err != nil {
		return nil, fmt.Errorf("mcp/client: stat auth token %q: %w", cfg.AuthTokenPath, err)
	}

	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, fmt.Errorf("mcp/client: auth token %q has insecure mode %#o (must be 0600 or stricter)",
			cfg.AuthTokenPath, mode)
	}

	tokenBytes, err := os.ReadFile(cfg.AuthTokenPath)
	if err != nil {
		return nil, fmt.Errorf("mcp/client: read auth token %q: %w", cfg.AuthTokenPath, err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return nil, fmt.Errorf("mcp/client: auth token file %q is empty", cfg.AuthTokenPath)
	}

	hosts := defaultAllowedHosts()
	for _, h := range cfg.AllowedHosts {
		hosts[strings.ToLower(h)] = struct{}{}
	}

	if cfg.BaseURL != "" {
		if u, err2 := parseHostFromURL(cfg.BaseURL); err2 == nil && u != "" {
			hosts[strings.ToLower(u)] = struct{}{}
		}
	}

	transport := buildTransport(cfg, hosts)

	c := &Client{
		cfg:   cfg,
		token: token,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		allowed: hosts,
	}
	return c, nil
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.token)

	if c.isDaemonRequest(req) {
		return c.doWithRetry(req)
	}
	return c.httpClient.Do(req)
}

func (c *Client) isDaemonRequest(req *http.Request) bool {
	h := strings.ToLower(req.URL.Hostname())

	if h == "" || h == "daemon" {
		return true
	}

	if c.cfg.BaseURL != "" {
		baseHost, err := parseHostFromURL(c.cfg.BaseURL)
		if err != nil {

			return false
		}
		return strings.EqualFold(strings.ToLower(req.URL.Host), strings.ToLower(baseHost))
	}
	return false
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < retryMaxAttempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * retryBaseDelay
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(delay):
			}

			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("mcp/client: re-read request body attempt %d: %w", attempt, err)
				}
				req.Body = body
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("mcp/client: daemon returned %d (attempt %d)", resp.StatusCode, attempt+1)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("mcp/client: all %d attempts failed, last: %w", retryMaxAttempts, lastErr)
}

func (c *Client) BaseURL() string {
	if c.cfg.BaseURL != "" {
		return c.cfg.BaseURL
	}
	return "http://daemon"
}

func (c *Client) MCPName() string {
	return c.cfg.MCPName
}

func (c *Client) Close() error {
	if c.httpClient == nil {
		return nil
	}
	if t, ok := c.httpClient.Transport.(*whitelistTransport); ok {
		if inner, ok := t.inner.(*http.Transport); ok {
			inner.CloseIdleConnections()
		}
	}
	return nil
}

const daemonSentinel = ""

// defaultAllowedHostsSealed is the authoritative whitelist (invariant).
// Changes to this set require an ADR (see architecture records
// for the precedent on how to extend it).
//
// Treat as immutable. Callers MUST go through cloneDefaultAllowedHosts()
// — never mutate this map directly. Review S-4 promoted this from a
// per-call function to a package-level var to make the immutability
// boundary explicit.
var defaultAllowedHostsSealed = map[string]struct{}{
	daemonSentinel:        {},
	"arxiv.org":           {},
	"export.arxiv.org":    {},
	"api.github.com":      {},
	"duckduckgo.com":      {},
	"html.duckduckgo.com": {},
}

func cloneDefaultAllowedHosts() map[string]struct{} {
	out := make(map[string]struct{}, len(defaultAllowedHostsSealed))
	for h := range defaultAllowedHostsSealed {
		out[h] = struct{}{}
	}
	return out
}

func defaultAllowedHosts() map[string]struct{} {
	return cloneDefaultAllowedHosts()
}

type whitelistTransport struct {
	inner   http.RoundTripper
	allowed map[string]struct{}
}

func (t *whitelistTransport) RoundTrip(req *http.Request) (*http.Response, error) {

	hostname := strings.ToLower(req.URL.Hostname())
	hostWithPort := strings.ToLower(req.URL.Host)

	_, okHost := t.allowed[hostname]
	_, okHostPort := t.allowed[hostWithPort]
	if !okHost && !okHostPort {

		if hostname != "" {
			return nil, fmt.Errorf("%w: %q", ErrHostNotAllowed, req.URL.Host)
		}
	}
	return t.inner.RoundTrip(req)
}

func buildTransport(cfg Config, allowed map[string]struct{}) http.RoundTripper {
	var inner http.RoundTripper

	if cfg.BaseURL != "" {

		inner = &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		}
	} else {

		socketPath := cfg.SocketPath
		inner = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		}
	}

	return &whitelistTransport{inner: inner, allowed: allowed}
}

func parseHostFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("empty host")
	}
	return u.Host, nil
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return home + path[1:]
}
