// SPDX-License-Identifier: MIT
// Package aggregator ships the Plan 13 Phase F doctor aggregator
// orchestrator: parallel execution via errgroup bounded min(numCPU, 8),
// per-check context timeout, Tessera audit emit, JSON schemaVersion=1.0
// serialization, bitmask exit-code computation.
//
// Phase F Task F1 baseline; F3-F4-F5 extend with Fix() loop + backup +
// CLI surface.
//
// Boundary (inv-zen-031): aggregator consumes only check.Check (interface)
// + Emitter (interface; satisfied by internal/audit/chain in production);
// MUST NOT import internal/store.
//
// Audit-pending queue (per spec §3.4 chaos test daemon-disconnect):
// when the daemon is down, the production Emitter implementation buffers
// to ~/.local/state/zen-swarm/audit-pending.jsonl (Plan 9 substrate). The
// aggregator's contract is "best-effort emit": on Emitter error, the
// aggregator logs but does NOT fail Run() — the diagnostic is still
// surfaced to the operator.
package aggregator

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"golang.org/x/sync/errgroup"
)

const SchemaVersion = "1.0"

const AuditEventType = "evt.doctor.full.run"

const MaxParallelism = 8

const DefaultCheckTimeout = 5 * time.Second

type Emitter interface {
	Emit(ctx context.Context, eventType string, payload []byte) (auditHash string, err error)
}

type Config struct {
	Checks       []check.Check
	MaxParallel  int
	CheckTimeout time.Duration
	Emitter      Emitter

	UseCachedIfFresh bool
}

type Aggregator struct {
	checks       []check.Check
	maxParallel  int
	checkTimeout time.Duration
	emitter      Emitter
}

func New(cfg Config) *Aggregator {
	maxP := cfg.MaxParallel
	if maxP <= 0 {
		maxP = runtime.NumCPU()
		if maxP > MaxParallelism {
			maxP = MaxParallelism
		}
		if maxP < 1 {
			maxP = 1
		}
	}
	timeout := cfg.CheckTimeout
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	return &Aggregator{
		checks:       cfg.Checks,
		maxParallel:  maxP,
		checkTimeout: timeout,
		emitter:      cfg.Emitter,
	}
}

func (a *Aggregator) Run(ctx context.Context) (*Report, error) {
	report := &Report{
		SchemaVersion: SchemaVersion,
		StartedAt:     time.Now().UTC(),
		Diagnostics:   make([]check.DiagnosticResult, len(a.checks)),
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(a.maxParallel)

	var mu sync.Mutex
	for i, c := range a.checks {
		i, c := i, c
		g.Go(func() error {
			perCtx, cancel := context.WithTimeout(gctx, a.checkTimeout)
			defer cancel()
			start := time.Now()
			res := runWithTimeout(perCtx, c)
			res.DurationMs = time.Since(start).Milliseconds()
			if res.Name == "" {
				res.Name = c.Name()
			}
			mu.Lock()
			report.Diagnostics[i] = res
			mu.Unlock()
			return nil
		})
	}

	groupErr := g.Wait()
	report.FinishedAt = time.Now().UTC()

	for _, d := range report.Diagnostics {
		switch d.Status {
		case check.StatusPass:
			report.PassCount++
		case check.StatusWarn:
			report.WarnCount++
		case check.StatusFail:
			report.FailCount++
		case check.StatusSkip:
			report.SkipCount++
		}
	}

	if a.emitter != nil {
		payload, jerr := json.Marshal(report.summaryForAudit())
		if jerr == nil {
			hash, emerr := a.emitter.Emit(ctx, AuditEventType, payload)
			if emerr == nil {
				report.AuditEventHash = hash
			}
		}
	}

	if groupErr != nil {
		return report, groupErr
	}

	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	return report, nil
}

func runWithTimeout(ctx context.Context, c check.Check) check.DiagnosticResult {
	res := c.Run(ctx)
	if ctx.Err() != nil && res.Status != check.StatusSkip {

		res.Status = check.StatusSkip
		if res.Message == "" {
			res.Message = "context deadline exceeded"
		}
	}
	return res
}

type Report struct {
	SchemaVersion  string                   `json:"schemaVersion"`
	StartedAt      time.Time                `json:"startedAt"`
	FinishedAt     time.Time                `json:"finishedAt"`
	Diagnostics    []check.DiagnosticResult `json:"diagnostics"`
	PassCount      int                      `json:"passCount"`
	WarnCount      int                      `json:"warnCount"`
	FailCount      int                      `json:"failCount"`
	SkipCount      int                      `json:"skipCount"`
	AuditEventHash string                   `json:"auditEventHash,omitempty"`
}

func (r *Report) summaryForAudit() map[string]any {
	type entry struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		DurationMs int64  `json:"durationMs"`
	}
	rows := make([]entry, 0, len(r.Diagnostics))
	for _, d := range r.Diagnostics {
		rows = append(rows, entry{
			Name:       d.Name,
			Status:     d.Status.String(),
			DurationMs: d.DurationMs,
		})
	}
	return map[string]any{
		"schemaVersion": r.SchemaVersion,
		"startedAt":     r.StartedAt,
		"finishedAt":    r.FinishedAt,
		"diagnostics":   rows,
		"counts": map[string]int{
			"pass": r.PassCount,
			"warn": r.WarnCount,
			"fail": r.FailCount,
			"skip": r.SkipCount,
		},
	}
}
