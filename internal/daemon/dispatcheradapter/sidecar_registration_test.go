// SPDX-License-Identifier: MIT
// internal/daemon/dispatcheradapter/sidecar_registration_test.go
//
// Tests for RegisterSidecars — the daemon-startup hook that reads
// sidecars.toml, probes /health, and registers the SidecarBackend by name
// into providers.Registry per invariant frozen contract C8 (name-based
// cascade; ProfileResolver determines order).

package dispatcheradapter_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// captureLogger returns a logger plus a buffer for tests that want to
// inspect log output (e.g. confirming a "skipped registration" warning
// fired).
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestRegisterSidecars_Healthy_RegistersBackend(t *testing.T) {
	healthHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			healthHits++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	reg := providers.NewRegistry()
	cfg := &config.Sidecars{
		Tier1Bypass: &config.Tier1BypassSidecar{
			URL:                        srv.URL,
			Tier:                       1,
			HealthProbeIntervalSeconds: 30,
			RequestTimeoutSeconds:      30,
			Required:                   false,
		},
	}
	dispatcheradapter.RegisterSidecars(context.Background(), reg, cfg, quietLogger())

	if healthHits != 1 {
		t.Errorf("health hits = %d; want exactly 1 probe", healthHits)
	}
	backend, err := reg.Get("bypass-sidecar")
	if err != nil {
		t.Fatalf("Registry.Get(bypass-sidecar): %v; want backend registered", err)
	}
	if backend.Name() != "bypass-sidecar" {
		t.Errorf("backend.Name() = %q; want bypass-sidecar", backend.Name())
	}
	if backend.Tier() != providers.TierInHouse {
		t.Errorf("backend.Tier() = %v; want TierInHouse", backend.Tier())
	}
}

func TestRegisterSidecars_NilConfig_NoOpDoesNotRegister(t *testing.T) {
	reg := providers.NewRegistry()
	dispatcheradapter.RegisterSidecars(context.Background(), reg, nil, quietLogger())
	if names := reg.List(); len(names) != 0 {
		t.Errorf("Registry.List() = %v; want empty after nil-cfg RegisterSidecars", names)
	}
}

func TestRegisterSidecars_NilTier1Bypass_NoOpDoesNotRegister(t *testing.T) {
	reg := providers.NewRegistry()
	cfg := &config.Sidecars{Tier1Bypass: nil}
	dispatcheradapter.RegisterSidecars(context.Background(), reg, cfg, quietLogger())
	if names := reg.List(); len(names) != 0 {
		t.Errorf("Registry.List() = %v; want empty for nil-tier1bypass cfg", names)
	}
}

// ----------------------------------------------------------------------------
// Unhealthy probe — skip registration; log warning; cascade owns the path.
// ----------------------------------------------------------------------------

// TestRegisterSidecars_HealthProbeNon200_SkipsRegistration asserts that
// a 503 (or any non-200) /health response causes the backend to be
// skipped: no registration, with a warning log line. The operator's
// daemon boots successfully; the cascade handles dispatches.
func TestRegisterSidecars_HealthProbeNon200_SkipsRegistration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	reg := providers.NewRegistry()
	cfg := &config.Sidecars{
		Tier1Bypass: &config.Tier1BypassSidecar{
			URL:                        srv.URL,
			Tier:                       1,
			HealthProbeIntervalSeconds: 30,
			RequestTimeoutSeconds:      30,
		},
	}
	log, buf := captureLogger()
	dispatcheradapter.RegisterSidecars(context.Background(), reg, cfg, log)

	if _, err := reg.Get("bypass-sidecar"); err == nil {
		t.Error("backend registered despite 503 /health; want skip")
	}
	if !strings.Contains(buf.String(), "bypass sidecar") || !strings.Contains(buf.String(), "skipping registration") {
		t.Errorf("warning log missing; got:\n%s", buf.String())
	}
}

func TestRegisterSidecars_ProbeConnectionRefused_SkipsRegistration(t *testing.T) {
	reg := providers.NewRegistry()
	cfg := &config.Sidecars{
		Tier1Bypass: &config.Tier1BypassSidecar{
			URL:                        "http://127.0.0.1:1",
			Tier:                       1,
			HealthProbeIntervalSeconds: 30,
			RequestTimeoutSeconds:      30,
		},
	}
	log, buf := captureLogger()
	dispatcheradapter.RegisterSidecars(context.Background(), reg, cfg, log)

	if _, err := reg.Get("bypass-sidecar"); err == nil {
		t.Error("backend registered despite connection-refused /health; want skip")
	}
	if !strings.Contains(buf.String(), "bypass sidecar") || !strings.Contains(buf.String(), "skipping registration") {
		t.Errorf("warning log missing; got:\n%s", buf.String())
	}
}

func TestRegisterSidecars_DuplicateName_LogsAndKeepsExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	reg := providers.NewRegistry()

	prior := providers.NewSidecarBackend("http://127.0.0.1:65535", 1)
	defer prior.Close()
	if err := reg.Register("bypass-sidecar", prior); err != nil {
		t.Fatalf("pre-register: %v", err)
	}
	cfg := &config.Sidecars{
		Tier1Bypass: &config.Tier1BypassSidecar{
			URL:                        srv.URL,
			Tier:                       1,
			HealthProbeIntervalSeconds: 30,
			RequestTimeoutSeconds:      30,
		},
	}
	log, buf := captureLogger()

	dispatcheradapter.RegisterSidecars(context.Background(), reg, cfg, log)

	// The pre-registered backend MUST still be the one in the registry
	// (idempotent contract — never silently overwrite).
	got, err := reg.Get("bypass-sidecar")
	if err != nil {
		t.Fatalf("Get after dup-register: %v", err)
	}
	if got != prior {
		t.Errorf("registry.Get(bypass-sidecar) returned a NEW instance; want the pre-registered one (idempotency)")
	}
	if !strings.Contains(buf.String(), "already registered") {
		t.Errorf("log missing 'already registered' message; got:\n%s", buf.String())
	}
}

// TestRegisterSidecars_PanicsOnNilRegistry asserts the fail-fast guard.
// Wiring bugs that pass nil deps MUST surface at boot, not at first
// dispatch (same posture as dispatcher.New).
func TestRegisterSidecars_PanicsOnNilRegistry(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("did not panic on nil reg; want panic")
		}
		if !strings.Contains(fmt.Sprintf("%v", r), "reg is nil") {
			t.Errorf("panic = %v; want one mentioning reg is nil", r)
		}
	}()
	dispatcheradapter.RegisterSidecars(context.Background(), nil, &config.Sidecars{}, quietLogger())
}

func TestRegisterSidecars_PanicsOnNilLogger(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("did not panic on nil log; want panic")
		}
		if !strings.Contains(fmt.Sprintf("%v", r), "log is nil") {
			t.Errorf("panic = %v; want one mentioning log is nil", r)
		}
	}()
	dispatcheradapter.RegisterSidecars(context.Background(), providers.NewRegistry(), &config.Sidecars{}, nil)
}

// TestRegisterSidecars_InvalidURL_SkipsRegistration asserts a malformed
// tier1.URL surfaces as a "skipping registration" warning rather than
// crashing the daemon. (Per validation, sidecars.toml
// loader normally catches this — but RegisterSidecars must be defensive
// against a caller that bypasses the loader, e.g. tests injecting a
// hand-built cfg.)
func TestRegisterSidecars_InvalidURL_SkipsRegistration(t *testing.T) {
	reg := providers.NewRegistry()
	cfg := &config.Sidecars{
		Tier1Bypass: &config.Tier1BypassSidecar{
			URL:                        "://invalid",
			Tier:                       1,
			HealthProbeIntervalSeconds: 30,
			RequestTimeoutSeconds:      30,
		},
	}
	log, buf := captureLogger()

	dispatcheradapter.RegisterSidecars(context.Background(), reg, cfg, log)

	if _, err := reg.Get("bypass-sidecar"); err == nil {
		t.Error("backend registered despite malformed URL; want skip")
	}
	if !strings.Contains(buf.String(), "skipping registration") {
		t.Errorf("warning log missing; got:\n%s", buf.String())
	}
}

func TestRegisterSidecars_ContextCancellation_SkipsRegistration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		<-r.Context().Done()
	}))
	defer srv.Close()

	reg := providers.NewRegistry()
	cfg := &config.Sidecars{
		Tier1Bypass: &config.Tier1BypassSidecar{
			URL:                        srv.URL,
			Tier:                       1,
			HealthProbeIntervalSeconds: 30,
			RequestTimeoutSeconds:      30,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	log, buf := captureLogger()
	dispatcheradapter.RegisterSidecars(ctx, reg, cfg, log)

	if _, err := reg.Get("bypass-sidecar"); err == nil {
		t.Error("backend registered despite cancelled ctx; want skip")
	}
	if !strings.Contains(buf.String(), "skipping registration") {
		t.Errorf("warning log missing; got:\n%s", buf.String())
	}
}
