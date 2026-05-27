// SPDX-License-Identifier: MIT
// tier_health_sink.go — orchestrator-side seam for per-provider health
// samples.
//
// TierHealthSink is the storage contract the RecoveryScheduler (probe
// outcomes) and the daemon's dispatcher-outcome wiring write through. The
// real implementation is dispatcheradapter.TierHealthSampleAdapter (Task 14),
// which translates to store.TierHealthSampleRow and forwards to
// store.InsertTierHealthSample.
//
// Boundary (invariant): the orchestrator package MUST NOT import
// internal/store. TierHealthSampleRow is an orchestrator-local mirror of
// store.TierHealthSampleRow (intentionally identical in shape) — the same
// "two type sets" discipline used for CostLedgerRow. The dispatcheradapter
// is the sole place the two row types meet; a reflective parity test there
// guards against drift.
package orchestrator

import "time"

type TierHealthSampleRow struct {
	TS           time.Time
	Provider     string
	Tier         string
	Success      bool
	LatencyMS    int64
	ErrorPattern string
}

// TierHealthSink receives one health sample per backend outcome. The real
// impl (dispatcheradapter) writes to tier_health_samples; tests use an
// in-memory recorder. RecordHealthSample errors are logged-and-swallowed by
// callers — a health-sample write blip MUST NOT shadow a real LLM response
// or crash the recovery scheduler.
type TierHealthSink interface {
	RecordHealthSample(row TierHealthSampleRow) error
}
