// SPDX-License-Identifier: MIT
// internal/orchestrator/recovery_heartbeat.go
//
// Spec §3.3 background goroutines: every Interval (default 30 s) the
// monitor polls the configured HeartbeatProbe for the {worker_id →
// last_beat} map; any worker whose last beat is older than Timeout
// (default 2×Interval = 60 s) is treated as dead. For each dead worker:
//
//  1. Emit an EvtWorkerDeath audit row with class=TRANSIENT_INFRA,
//     reason="heartbeat_timeout", task_id=last-known-assignment, and
//     retry_count=0 (RecoveryEngine fills the cumulative retry count
//     in the paired EvtWorkerRedispatched row).
//  2. Invoke RecoveryEngine.HandleWorkerDeath with the canonical
//     sentinel ErrHeartbeatTimeout (classified as TRANSIENT_INFRA per
//     Task E-1 Classify rule 2). The engine applies the per-doctrine
//     retry budget and emits the WorkerRedispatched row.
//
// The two-event pair (Death + Redispatched) is the load-bearing
// integration contract for Phase E-6 replay reconstruction: replay
// pairs them by worker_id+task_id to determine which workers still
// need re-dispatch after orchestrator restart.
//
// Lifecycle Run blocks until ctx is cancelled and exits within one
// tick on cancel (the select observes ctx.Done() at the same priority
// as ticker.C(); ticker.Stop() is deferred to release the underlying
// pendingFire). Goleak-verified clean shutdown in every test.
//
// Probe-error handling: a non-nil error from probe.LastBeats is
// SWALLOWED (loop continues); the orchestrator's separate probe-health
// path is responsible for surfacing the probe-substrate failure as its
// own audit row. Without this swallow, a transient probe fault would
// kill the monitor goroutine (silent loss of recovery coverage).
//
// Audit-trail discipline (D-2/D-3 carry-forward): the Death emission
// uses context.WithoutCancel(ctx) so a cancelled caller-ctx (test
// shutdown, orchestrator drain) never drops the forensic row. The
// HandleWorkerDeath call still receives the live ctx so the engine's
// downstream logic respects cancellation; that path's own
// WorkerRedispatched emission also uses WithoutCancel internally.
package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// HeartbeatProbe is the read-only liveness view of Plan 4 worker (and
// future reviewer) subprocesses. The real implementation in Plan 4
// queries worker.SubprocessManager for the in-memory last-beat
// timestamp map; tests inject deterministic fakes (see fakeProbe).
//
// Returned timestamps are in the clock the probe writes them with —
// the heartbeat monitor compares against its own injected clock.Clock,
// so production callers MUST drive both with the same clock (real
// time) and tests inject the same *clock.Fake into both the probe
// data and the monitor.
//
// Returning a non-nil error signals a transient probe-substrate fault
// (e.g. socket recv failure, RPC timeout). The monitor swallows the
// error and continues to the next tick; surfacing it as its own audit
// row is the orchestrator's probe-health responsibility.
type HeartbeatProbe interface {
	LastBeats(ctx context.Context) (map[string]time.Time, error)
}

type HeartbeatMonitor struct {
	eng      *RecoveryEngine
	probe    HeartbeatProbe
	interval time.Duration
	timeout  time.Duration
	clk      clock.Clock
}

type HeartbeatConfig struct {
	Engine   *RecoveryEngine
	Probe    HeartbeatProbe
	Interval time.Duration
	Timeout  time.Duration
	Clock    clock.Clock
}

func NewHeartbeatMonitor(cfg HeartbeatConfig) (*HeartbeatMonitor, error) {
	if cfg.Engine == nil {
		return nil, fmt.Errorf("%w: engine is nil", ErrInvalidConfig)
	}
	if cfg.Probe == nil {
		return nil, fmt.Errorf("%w: probe is nil", ErrInvalidConfig)
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * cfg.Interval
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	return &HeartbeatMonitor{
		eng:      cfg.Engine,
		probe:    cfg.Probe,
		interval: cfg.Interval,
		timeout:  cfg.Timeout,
		clk:      cfg.Clock,
	}, nil
}

func (m *HeartbeatMonitor) Run(ctx context.Context) {
	ticker := m.clk.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			m.checkOnce(ctx)
		}
	}
}

// checkOnce performs one liveness sweep. For each stale worker
// (now - lastBeat > timeout) it emits a WorkerDeath audit row and
// invokes RecoveryEngine.HandleWorkerDeath.
//
// Probe error: swallowed (loop continues). The Append return is
// ignored: a transient audit-store failure is observed elsewhere
// (engine.OnCorruption / inv-zen-095), and we MUST still drive the
// recovery engine so the doctrine retry budget advances even when
// the audit emit lost the row. The HandleWorkerDeath call's own
// audit emission has the same discipline.
func (m *HeartbeatMonitor) checkOnce(ctx context.Context) {
	beats, err := m.probe.LastBeats(ctx)
	if err != nil {

		return
	}
	now := m.clk.Now()
	for workerID, lastBeat := range beats {
		if now.Sub(lastBeat) <= m.timeout {
			continue
		}

		taskID := m.eng.LastAssignmentFor(ctx, workerID)

		auditCtx := context.WithoutCancel(ctx)
		_, _ = m.eng.evlog.Append(auditCtx, eventlog.Event{
			Type:      eventlog.EvtWorkerDeath,
			SessionID: m.eng.sessionID,
			ProjectID: m.eng.projectID,
			Timestamp: now,
			Payload: map[string]any{
				"worker_id":   workerID,
				"task_id":     taskID,
				"class":       FailureTransientInfra.String(),
				"reason":      "heartbeat_timeout",
				"retry_count": 0,
			},
		})

		_, _ = m.eng.HandleWorkerDeath(auditCtx, WorkerDeathInput{
			TaskID:   taskID,
			WorkerID: workerID,
			Err:      ErrHeartbeatTimeout,
		})
	}
}
