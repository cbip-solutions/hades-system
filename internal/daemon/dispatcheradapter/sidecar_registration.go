// SPDX-License-Identifier: MIT
// internal/daemon/dispatcheradapter/sidecar_registration.go
//
// RegisterSidecars is the daemon-startup hook that reads sidecars.toml
// (`config.Sidecars`), probes the declared sidecar's /health endpoint, and
// registers the SidecarBackend by name into the providers.Registry.
//
// Architectural choice — name-based registration (per invariant frozen
// contract C8): the HADES design → HADES design cascade refactor moved from a
// 2-tier hard-wire ("tier1 + tier2") to a name-based ProfileResolver
// model. RegisterSidecars MUST register backends by name into
// providers.Registry; the operator's profiles.toml determines which
// profiles include "bypass-sidecar" in their cascade and at which
// position. There is NO "RegisterTier1" method anywhere in the codebase
// (an earlier plan version cited one — that is plan-template drift).
//
// Graceful-degradation discipline (invariant):
// - cfg == nil OR cfg.Tier1Bypass == nil → no-op; the HADES design cascade
// handles the path. Operators without sidecars.toml are the common
// case.
// - /health probe fails (5xx, transport error, ctx cancel) → log a
// warning + skip registration. The daemon boots successfully and
// the cascade handles dispatches. The operator's `hades status` (a
// future surface) reports the sidecar as DOWN.
// - duplicate "bypass-sidecar" name (e.g. a wiring race with a
// pre-registered backend) → log a warning + keep the existing
// instance (mirrors the bypass-backend idempotency precedent at
// cmd/hades-ctld/orchestrator_wiring.go:438).
//
// invariant boundary: this file imports internal/config + internal/providers +
// stdlib. It does NOT import internal/store.
//
// Concurrency RegisterSidecars is invoked once at daemon boot from
// the single-threaded wiring path; no internal locking. The probe HTTP
// client is constructed locally; no shared state.

package dispatcheradapter

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

const sidecarProbeTimeout = 5 * time.Second

type SidecarRegistry interface {
	Register(name string, b providers.TierBackend) error

	Get(name string) (providers.TierBackend, error)
}

// RegisterSidecars reads sidecars.toml (loaded via config.LoadSidecars)
// and registers a SidecarBackend per declared sidecar after a successful
// /health probe.
//
// Only the [tier1.bypass] sidecar is consumed; future tier sidecars extend
// this function with additional probe + register branches.
//
// Pre-conditions:
// - reg MUST be non-nil (panics otherwise — same fail-fast posture as
// dispatcher.New / dispatcheradapter.New).
// - log MUST be non-nil.
// - ctx is forwarded to the probe HTTP request; a cancelled ctx causes
// skip rather than hang.
//
// Post-conditions:
// - On healthy probe: reg has "bypass-sidecar" registered with a
// SidecarBackend whose Forward/Probe target cfg.Tier1Bypass.URL +
// RequestTimeoutSeconds.
// - On any failure (nil cfg, nil Tier1Bypass, probe failure, register
// failure): reg is unchanged (no partial registration); a log line
// describes the outcome.
func RegisterSidecars(ctx context.Context, reg SidecarRegistry, cfg *config.Sidecars, log *slog.Logger) {
	if reg == nil {
		panic("dispatcheradapter.RegisterSidecars: reg is nil")
	}
	if log == nil {
		panic("dispatcheradapter.RegisterSidecars: log is nil")
	}

	if cfg == nil || cfg.Tier1Bypass == nil {

		log.Info("no tier1 bypass sidecar configured; relying on HADES design cascade")
		return
	}

	tier1 := cfg.Tier1Bypass
	healthURL, err := url.JoinPath(tier1.URL, "/health")
	if err != nil {
		log.Warn("bypass sidecar health probe URL invalid; skipping registration",
			"url", tier1.URL, "err", err)
		return
	}

	probeClient := &http.Client{Timeout: sidecarProbeTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		log.Warn("bypass sidecar health probe request invalid; skipping registration",
			"url", healthURL, "err", err)
		return
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		log.Warn("bypass sidecar health probe failed; skipping registration",
			"url", healthURL, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Warn("bypass sidecar health probe non-200; skipping registration",
			"url", healthURL, "status", resp.StatusCode)
		return
	}

	backend := providers.NewSidecarBackend(tier1.URL, time.Duration(tier1.RequestTimeoutSeconds)*time.Second)
	if err := reg.Register(backend.Name(), backend); err != nil {

		log.Warn("bypass sidecar already registered; keeping existing instance",
			"name", backend.Name(), "err", err)

		_ = backend.Close()
		return
	}
	log.Info("bypass sidecar registered",
		"name", backend.Name(),
		"url", tier1.URL,
		"timeout_s", tier1.RequestTimeoutSeconds,
		"health_probe_interval_s", tier1.HealthProbeIntervalSeconds,
		"required", tier1.Required)
}

var _ SidecarRegistry = (*providers.Registry)(nil)
