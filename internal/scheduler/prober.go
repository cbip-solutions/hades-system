// SPDX-License-Identifier: MIT
// Package scheduler — prober.go
//
// Phase J Task J-4 adapter: exposes a slim Prober implementation that
// the cli/doctor_scheduler.go layer consumes (cli.SchedulerProber). The
// split keeps inv-zen-031 clean (internal/cli imports internal/scheduler;
// internal/scheduler does NOT import internal/store).
//
// Phase J prefers closure injection over Store interface accretion.
// Each subsystem-specific query (queue depth, missed fires) flows
// through a function value supplied by the daemon main loop, which
// already owns the *store.Store handle. This keeps Phase D's Store
// interface minimal (CRUD + ListDue + AppendHistory) and isolates
// doctor-only queries from the production hot path.
//
// The Prober is read-only: every method either reads via injected
// closure or calls a pre-bound dispatcher health probe. No mutations.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// QueueDepthFn returns total pending schedules + per-project breakdown.
// Pending = next_run_at <= now() AND status='enabled'. Wired by the
// daemon over the *scheduleradapter Adapter (the boundary crosser).
//
// MUST be safe for concurrent use; multiple operators may run
// `zen doctor scheduler` concurrently.
type QueueDepthFn func(ctx context.Context, now time.Time) (total int, byProject map[string]int, err error)

// MissedFiresFn returns count of MissedFire events in the [since, now]
// window + per-project breakdown. Wired by the daemon over the
// scheduleradapter (which queries schedule_history rows with
// outcome='missed').
//
// MUST be safe for concurrent use.
type MissedFiresFn func(ctx context.Context, since time.Time) (total int, byProject map[string]int, err error)

// WfqStatusFn returns the maximum WFQ saturation % across active queues
// + the alias of the project at that max. Wired over Phase B
// quota.WfqStatus.
//
// MUST be safe for concurrent use.
type WfqStatusFn func(ctx context.Context) (maxPct int, maxAlias string, err error)

// DispatcherPingFn verifies the Plan 3 dispatcher is bound + reachable
// at the daemon's HTTP level. Returns nil if reachable; error otherwise.
// Wired by the daemon over the dispatcheradapter health probe.
//
// MUST be safe for concurrent use.
type DispatcherPingFn func(ctx context.Context) error

var ErrProberNilArg = errors.New("scheduler.NewProber: nil argument")

type Prober struct {
	queueDepth     QueueDepthFn
	missedFires    MissedFiresFn
	wfqStatus      WfqStatusFn
	dispatcherPing DispatcherPingFn
}

// NewProber wires a Prober. The four closures cover the four aspects
// surfaced by RunSchedulerProbe; the daemon main loop owns the actual
// store + quota + dispatcher handles and produces these closures at
// boot.
//
// Caller MUST NOT call Close on the Prober — there is no Close. The
// closures' underlying resources are managed by the daemon.
//
// Panics on any nil closure: programmer error at boot. The daemon main
// loop is the single canonical caller; partial-bootstrap state is a
// loud-failure condition.
func NewProber(
	queueDepth QueueDepthFn,
	missedFires MissedFiresFn,
	wfqStatus WfqStatusFn,
	dispatcherPing DispatcherPingFn,
) *Prober {
	if queueDepth == nil {
		panic(fmt.Errorf("%w: queueDepth", ErrProberNilArg))
	}
	if missedFires == nil {
		panic(fmt.Errorf("%w: missedFires", ErrProberNilArg))
	}
	if wfqStatus == nil {
		panic(fmt.Errorf("%w: wfqStatus", ErrProberNilArg))
	}
	if dispatcherPing == nil {
		panic(fmt.Errorf("%w: dispatcherPing", ErrProberNilArg))
	}
	return &Prober{
		queueDepth:     queueDepth,
		missedFires:    missedFires,
		wfqStatus:      wfqStatus,
		dispatcherPing: dispatcherPing,
	}
}

func (p *Prober) QueueDepth(ctx context.Context) (int, map[string]int, error) {
	return p.queueDepth(ctx, time.Now())
}

func (p *Prober) MissedFires24h(ctx context.Context) (int, map[string]int, error) {
	since := time.Now().Add(-24 * time.Hour)
	return p.missedFires(ctx, since)
}

func (p *Prober) WfqSaturation(ctx context.Context) (int, string, error) {
	return p.wfqStatus(ctx)
}

func (p *Prober) DispatcherBound(ctx context.Context) error {
	if err := p.dispatcherPing(ctx); err != nil {
		return fmt.Errorf("scheduler.Prober.DispatcherBound: %w", err)
	}
	return nil
}
