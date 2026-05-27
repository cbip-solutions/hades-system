// SPDX-License-Identifier: MIT
// internal/daemon/orchestrator/recovery_scheduler.go
//
// RecoveryScheduler periodically attempts to recover Open or Suspect providers
// via probing, keyed by provider name. The daemon spawns it on start and
// cancels it on shutdown.
//
// Design decision:
//
// The plan reference (line 677) specifies Run(ctx context.Context) as a
// blocking void method — caller must wrap in "go scheduler.Run(ctx)". This
// implementation deviates by returning <-chan struct{} (like F-7's
// StartHourlyMaintenance), spawning the goroutine internally. Rationale:
//
// 1. Alignment with F-7 StartHourlyMaintenance: one consistent pattern for
// all background goroutines in the orchestrator package eliminates an
// ad-hoc go call at each call site.
// 2. Graceful shutdown: the daemon's Stop path can "done := scheduler.Run(ctx);
// cancel(); <-done" to confirm the scheduler exited before returning, the
// same idiom used for maintenance goroutines.
// 3. D-6 wiring: "go scheduler.Run(ctx)" becomes "done := scheduler.Run(ctx)"
// — the fire-and-forget path still works (caller ignores the channel),
// and the graceful path is available without retrofitting.
//
// This deviation is pinned by TestRecoveryScheduler_GracefulShutdown and
// documented in the commit body.
//
// Boundary: this file imports stdlib (context, time) and
// internal/providers only. The orchestrator package MUST NOT import
// internal/store directly; boundary crossings flow through
// internal/daemon/dispatcheradapter.

package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/cbip-solutions/hades-system/internal/providers"
)

const defaultRecoveryInterval = 30 * time.Second

type RecoveryScheduler struct {
	breaker    *CircuitBreaker
	backends   []providers.TierBackend
	interval   time.Duration
	healthSink TierHealthSink
}

func NewRecoveryScheduler(cb *CircuitBreaker, backends []providers.TierBackend, interval time.Duration) *RecoveryScheduler {
	if interval <= 0 {
		interval = defaultRecoveryInterval
	}
	return &RecoveryScheduler{
		breaker:  cb,
		backends: backends,
		interval: interval,
	}
}

func (rs *RecoveryScheduler) SetHealthSink(sink TierHealthSink) {
	rs.healthSink = sink
}

// recordProbeSample writes one tier_health_samples row for a probe
// outcome. healed == true means the probe succeeded (the breaker is now
// Closed). A nil healthSink is a no-op. Sink errors are logged-and-
// swallowed: a health-sample write failure MUST NOT crash the recovery
// loop or affect breaker state.
func (rs *RecoveryScheduler) recordProbeSample(backend providers.TierBackend, healed bool, latency time.Duration) {
	if rs.healthSink == nil {
		return
	}
	pattern := ""
	if !healed {
		pattern = "probe-failed"
	}
	if err := rs.healthSink.RecordHealthSample(TierHealthSampleRow{
		TS:           time.Now(),
		Provider:     backend.Name(),
		Tier:         backend.Tier().String(),
		Success:      healed,
		LatencyMS:    latency.Milliseconds(),
		ErrorPattern: pattern,
	}); err != nil {
		slog.Warn("recovery scheduler: health sample write failed",
			"provider", backend.Name(), "error", err)
	}
}

func (rs *RecoveryScheduler) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(rs.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for _, backend := range rs.backends {

					st := rs.breaker.State(backend.Name())
					if st == StateClosed || st == StateRateLimited {
						continue
					}

					start := time.Now()
					healed := rs.breaker.AttemptRecovery(ctx, backend)
					rs.recordProbeSample(backend, healed, time.Since(start))
				}
			}
		}
	}()
	return done
}
