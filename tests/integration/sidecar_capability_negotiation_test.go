// SPDX-License-Identifier: MIT
// Package integration_test — Phase B Task B-10 capability-negotiation
// integration test for the daemon ↔ sidecar HTTP contract.
//
// Per decisión 17-d, the sidecar↔daemon contract over `/v1` is FROZEN
// FOREVER post-Phase-B-3 ship. Forward compatibility is achieved
// exclusively via `GET /v1/sidecar/info` capability negotiation: new
// sidecar releases announce features by adding entries to
// `supported_features[]`; older daemons gracefully ignore unknown
// flags. The capability vector is the load-bearing forward-surface
// mechanism — it is what allows Anthropic upstream body-shape changes
// to be absorbed inside the sidecar without ever bumping the
// `/v1` route prefix.
//
// This test (inv-zen-B7 placeholder; concrete inv-zen-NNN allocated at
// merge-time renumber reconciliation per the renumber-on-merge playbook)
// exercises an end-to-end flow:
//
//  1. Spin a FakeSidecar httptest.NewServer that serves
//     `GET /v1/sidecar/info` with a capability vector advertising the
//     5 required current flags PLUS a future-feature flag the daemon
//     does not yet recognise.
//  2. Invoke `dispatcheradapter.FetchSidecarCapabilities(baseURL)` —
//     the canonical daemon-side capability-fetch entry point.
//  3. Assert: the daemon returns the parsed `Capabilities`, does NOT
//     crash on the unknown feature, AND `HasFeature` reports correctly
//     for known + unknown flags (forward-compat property).
//  4. Assert: when the sidecar returns 5xx / malformed JSON / no
//     `/v1/sidecar/info` route, the daemon surfaces a typed error
//     rather than crashing (graceful degradation per inv-zen-280).
//
// inv-zen-031 boundary: this test imports
// internal/daemon/dispatcheradapter (daemon-side capability fetch) +
// stdlib. It does NOT import internal/store / private-tier1-module.
package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
)

var requiredCurrentFeatures = []string{
	"v0.17.8_refresh_protocol",
	"v0.17.9_fingerprint_coexistence",
	"v0.17.10_metadata_user_id",
	"v0.17.11_response_decompression",
	"v0.17.11_validator_classifier",
}

func TestSidecarCapabilityNegotiation_ForwardCompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sidecar/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": "v0.99.0",
			"supported_features": append(
				append([]string{}, requiredCurrentFeatures...),
				"v9.99.99_future_feature_unknown_to_daemon",
			),
			"config_hash":                    "abc123",
			"bypass_configs_version":         "2026.05.24",
			"anthropic_api_envelope_version": "2027-12-31",
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	caps, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err != nil {
		t.Fatalf("FetchSidecarCapabilities: %v; want nil err on happy path", err)
	}
	if caps.Version != "v0.99.0" {
		t.Errorf("caps.Version = %q; want v0.99.0", caps.Version)
	}
	for _, feat := range requiredCurrentFeatures {
		if !caps.HasFeature(feat) {
			t.Errorf("HasFeature(%q) = false; want true (required current feature)", feat)
		}
	}

	if !caps.HasFeature("v9.99.99_future_feature_unknown_to_daemon") {
		t.Error("HasFeature(future) = false; want true (vector is opaque, the flag IS advertised)")
	}

	if caps.HasFeature("v0.0.0_never_existed") {
		t.Error("HasFeature(never_existed) = true; want false")
	}
	if caps.ConfigHash != "abc123" {
		t.Errorf("caps.ConfigHash = %q; want abc123", caps.ConfigHash)
	}
	if caps.BypassConfigsVersion != "2026.05.24" {
		t.Errorf("caps.BypassConfigsVersion = %q; want 2026.05.24", caps.BypassConfigsVersion)
	}
	if caps.AnthropicAPIEnvelopeVersion != "2027-12-31" {
		t.Errorf("caps.AnthropicAPIEnvelopeVersion = %q; want 2027-12-31", caps.AnthropicAPIEnvelopeVersion)
	}
}

func TestSidecarCapabilityNegotiation_Sidecar5xx_GracefulError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err on 503; want typed error")
	}
	if !strings.Contains(err.Error(), "503") && !strings.Contains(err.Error(), "status") {
		t.Errorf("err = %v; want error mentioning status / 503", err)
	}
}

func TestSidecarCapabilityNegotiation_MalformedJSON_GracefulError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sidecar/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json-at-all{"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err on malformed JSON; want decode error")
	}
}

func TestSidecarCapabilityNegotiation_MissingInfoRoute_GracefulError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err on missing /v1/sidecar/info; want 404 error")
	}
}

func TestSidecarCapabilityNegotiation_ConnectionRefused_GracefulError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	baseURL := srv.URL
	srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, baseURL)
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err on closed server; want transport error")
	}
}

func TestSidecarCapabilityNegotiation_EmptyCapabilityVector_ForwardCompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sidecar/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":            "v0.1.0-minimal",
			"supported_features": []string{},
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	caps, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err != nil {
		t.Fatalf("FetchSidecarCapabilities: %v; want nil err on empty vector", err)
	}
	for _, feat := range requiredCurrentFeatures {
		if caps.HasFeature(feat) {
			t.Errorf("HasFeature(%q) = true; want false on empty vector (reverse-compat property)", feat)
		}
	}
	if caps.Version != "v0.1.0-minimal" {
		t.Errorf("caps.Version = %q; want v0.1.0-minimal", caps.Version)
	}
}
