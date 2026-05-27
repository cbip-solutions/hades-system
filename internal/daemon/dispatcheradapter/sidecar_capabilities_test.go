// SPDX-License-Identifier: MIT
// internal/daemon/dispatcheradapter/sidecar_capabilities_test.go
//
// Unit tests for FetchSidecarCapabilities + Capabilities.HasFeature.
// End-to-end behavioural assertions live in
// tests/integration/sidecar_capability_negotiation_test.go (B-10
// integration); these unit tests are the coverage anchor for the new
// surface and the unit-test pair canonical pattern (every NEW.go file
// in internal/daemon/ ships with a co-located _test.go per repo
// convention).

package dispatcheradapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
)

func TestCapabilities_HasFeature_KnownFlagReturnsTrue(t *testing.T) {
	caps := dispatcheradapter.Capabilities{
		SupportedFeatures: []string{"v0.17.8_refresh_protocol", "v0.17.11_response_decompression"},
	}
	if !caps.HasFeature("v0.17.8_refresh_protocol") {
		t.Error("HasFeature(known) = false; want true")
	}
}

func TestCapabilities_HasFeature_UnknownFlagReturnsFalse(t *testing.T) {
	caps := dispatcheradapter.Capabilities{
		SupportedFeatures: []string{"v0.17.8_refresh_protocol"},
	}
	if caps.HasFeature("v0.0.0_never_existed") {
		t.Error("HasFeature(unknown) = true; want false")
	}
}

func TestCapabilities_HasFeature_NilSliceReturnsFalse(t *testing.T) {
	var caps dispatcheradapter.Capabilities
	if caps.HasFeature("anything") {
		t.Error("HasFeature on zero-value = true; want false")
	}
}

func TestCapabilities_HasFeature_EmptyStringQuery(t *testing.T) {
	caps := dispatcheradapter.Capabilities{
		SupportedFeatures: []string{"v0.17.8_refresh_protocol"},
	}
	if caps.HasFeature("") {
		t.Error("HasFeature(empty) = true; want false")
	}
}

func TestFetchSidecarCapabilities_HappyPath_Returns200JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sidecar/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":                        "v0.17.11",
			"supported_features":             []string{"v0.17.8_refresh_protocol"},
			"config_hash":                    "deadbeef",
			"bypass_configs_version":         "2026.05.24",
			"anthropic_api_envelope_version": "2027-12-31",
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	caps, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err != nil {
		t.Fatalf("FetchSidecarCapabilities: %v; want nil", err)
	}
	if caps.Version != "v0.17.11" {
		t.Errorf("Version = %q; want v0.17.11", caps.Version)
	}
	if !caps.HasFeature("v0.17.8_refresh_protocol") {
		t.Error("HasFeature(v0.17.8_refresh_protocol) = false; want true")
	}
	if caps.ConfigHash != "deadbeef" {
		t.Errorf("ConfigHash = %q; want deadbeef", caps.ConfigHash)
	}
	if caps.BypassConfigsVersion != "2026.05.24" {
		t.Errorf("BypassConfigsVersion = %q; want 2026.05.24", caps.BypassConfigsVersion)
	}
	if caps.AnthropicAPIEnvelopeVersion != "2027-12-31" {
		t.Errorf("AnthropicAPIEnvelopeVersion = %q; want 2027-12-31", caps.AnthropicAPIEnvelopeVersion)
	}
}

func TestFetchSidecarCapabilities_Returns5xx_TypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err; want error")
	}
	if !errors.Is(err, dispatcheradapter.ErrCapabilityFetch) {
		t.Errorf("err = %v; want wraps ErrCapabilityFetch", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v; want substring '503'", err)
	}
}

func TestFetchSidecarCapabilities_MalformedJSON_TypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sidecar/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not-json"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err; want decode error")
	}
	if !errors.Is(err, dispatcheradapter.ErrCapabilityFetch) {
		t.Errorf("err = %v; want wraps ErrCapabilityFetch", err)
	}
}

func TestFetchSidecarCapabilities_InvalidBaseURL_TypedError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, "://not-a-valid-url")
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err on invalid url; want error")
	}
	if !errors.Is(err, dispatcheradapter.ErrCapabilityFetch) {
		t.Errorf("err = %v; want wraps ErrCapabilityFetch", err)
	}
}

func TestFetchSidecarCapabilities_ConnectionRefused_TypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	baseURL := srv.URL
	srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, baseURL)
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err on closed server; want transport error")
	}
	if !errors.Is(err, dispatcheradapter.ErrCapabilityFetch) {
		t.Errorf("err = %v; want wraps ErrCapabilityFetch", err)
	}
}

func TestFetchSidecarCapabilities_CtxCancelled_TypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err == nil {
		t.Fatal("FetchSidecarCapabilities returned nil err on cancelled ctx; want error")
	}
}

func TestFetchSidecarCapabilities_EmptyVector_ParsesAsZeroFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sidecar/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"version":"v0.1.0"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	caps, err := dispatcheradapter.FetchSidecarCapabilities(ctx, srv.URL)
	if err != nil {
		t.Fatalf("FetchSidecarCapabilities: %v; want nil err on minimal vector", err)
	}
	if caps.Version != "v0.1.0" {
		t.Errorf("Version = %q; want v0.1.0", caps.Version)
	}
	if caps.HasFeature("anything") {
		t.Error("HasFeature on empty vector = true; want false")
	}
}
