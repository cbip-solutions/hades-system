// SPDX-License-Identifier: MIT
// internal/daemon/dispatcheradapter/sidecar_capabilities.go
//
// FetchSidecarCapabilities is the daemon-side capability-fetch entry
// point per decisión 17-d ("/v1 frozen forever; forward-compat via
// capability negotiation"). The sidecar serves `GET /v1/sidecar/info`
// with a JSON capability vector (version + supported_features[] +
// config_hash + bypass_configs_version + anthropic_api_envelope_version).
// The daemon reads + feature-flags downstream behaviour accordingly.
//
// Forward-compat property (inv-zen-B7 placeholder; concrete inv-zen-NNN
// allocated at Plan 15 merge-time renumber reconciliation):
//
//   - New sidecar releases announce new behaviour by adding entries to
//     `supported_features[]`. Older daemons see the unknown flags as
//     opaque strings (HasFeature returns true) but never branch on
//     them — their feature-flag dispatch table only references known
//     flag names.
//   - This is what allows Anthropic upstream body-shape changes to be
//     absorbed inside the sidecar without ever bumping `/v1` to `/v2`.
//
// Graceful degradation (inv-zen-280): every error path returns a
// typed error so the daemon can continue without sidecar feature flags
// — the Plan 16 cascade is the natural fallback when the sidecar is
// unreachable / degraded / malformed.
//
// inv-zen-031 boundary: this file imports stdlib only (net/http,
// encoding/json, context, errors, fmt, io, net/url, time). It does NOT
// import internal/store, private-tier1-module, or
// internal/providers — the capabilities vector is daemon-owned state
// independent of the dispatcher's TierBackend cascade.
//
// Concurrency FetchSidecarCapabilities constructs a local http.Client
// per call; no shared state, safe for concurrent use. The daemon's
// boot path invokes it once during startup; the orchestrator may
// re-invoke periodically to refresh the cached vector.

package dispatcheradapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// capabilityFetchTimeout is the upper bound on a single
// `/v1/sidecar/info` GET. Sized to match sidecarProbeTimeout (5s) so
// boot-time + steady-state probes share a consistent SLA; the sidecar
// MUST respond within this window or it is treated as degraded.
const capabilityFetchTimeout = 5 * time.Second

var ErrCapabilityFetch = errors.New("sidecar capability fetch failed")

type Capabilities struct {
	Version                     string   `json:"version"`
	SupportedFeatures           []string `json:"supported_features"`
	ConfigHash                  string   `json:"config_hash"`
	BypassConfigsVersion        string   `json:"bypass_configs_version"`
	AnthropicAPIEnvelopeVersion string   `json:"anthropic_api_envelope_version"`
}

func (c Capabilities) HasFeature(name string) bool {
	for _, f := range c.SupportedFeatures {
		if f == name {
			return true
		}
	}
	return false
}

func FetchSidecarCapabilities(ctx context.Context, baseURL string) (Capabilities, error) {
	endpoint, err := url.JoinPath(baseURL, "/v1/sidecar/info")
	if err != nil {
		return Capabilities{}, fmt.Errorf("%w: url join: %w", ErrCapabilityFetch, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Capabilities{}, fmt.Errorf("%w: new request: %w", ErrCapabilityFetch, err)
	}

	client := &http.Client{Timeout: capabilityFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return Capabilities{}, fmt.Errorf("%w: transport: %w", ErrCapabilityFetch, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Capabilities{}, fmt.Errorf("%w: status %d", ErrCapabilityFetch, resp.StatusCode)
	}

	const maxBodyBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Capabilities{}, fmt.Errorf("%w: read body: %w", ErrCapabilityFetch, err)
	}

	var caps Capabilities
	if err := json.Unmarshal(body, &caps); err != nil {
		return Capabilities{}, fmt.Errorf("%w: decode: %w", ErrCapabilityFetch, err)
	}
	return caps, nil
}
