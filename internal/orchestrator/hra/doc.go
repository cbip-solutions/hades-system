// SPDX-License-Identifier: MIT
// Package hra implements the 4-layer Hierarchical Reviewer Assembly
// coordinator. The coordinator drives the
// continuous-async cadence per spec §5.4.6 doctrine matrix and turns
// isolated reviewer findings into doctrine-shaped signal flowing up
// the L1 → L2 → L3 → L4 chain.
//
// # Layers
//
// exposes three reviewer layers as Layer values:
//
// LayerTactical (L2) — consumes EvtWorkerCheckpoint
// LayerStrategic (L3) — consumes EvtReviewerWaveComplete
// LayerArchitectural (L4) — consumes EvtTacticalAggregation +
// EvtStrategicAggregation
//
// L1 is the worker layer that produces EvtWorkerCheckpoint upstream of
// the coordinator; the coordinator is the supervisor for L2/L3/L4 and
// does not own the L1 worker fleet directly.
//
// # Doctrine cadence matrix (spec §5.4.6)
//
// doctrine T (tactical) S (strategic) A (architectural)
// max-scope 3 min 10 min 30 min
// default 5 min 0 (skip) 0 (phase boundary)
// capa-firewall 0 (manual) 0 (manual) 0 (manual)
//
// A zero duration means the cadence goroutine is NOT auto-started for
// that layer; the layer is driven by explicit Tick() / phase-boundary
// triggers (wired in H-2..H-4 + H-8). The capa-firewall row is
// manual-only per Pulido §3.5 claim-strength tier — release wires the
// checklist runner that issues the Tick calls.
//
// # Subscriber pattern (spec §1 Q5 C)
//
// On Run, the coordinator registers three Subscriptions against the
// eventlog. Each Subscription is filtered by ProjectID
// and a layer-
// specific EventType set so per-layer cadence loops only see their
// upstream signal. Subscriptions are closed when Run's context is
// cancelled; the Done() channel is the canonical termination signal
// per the eventlog package contract.
//
// # Boundary discipline (inv-hades-089, inv-hades-090)
//
// imports only internal/orchestrator/clock and
// internal/orchestrator/eventlog. It never imports
// internal/orchestrator or internal/store —
// the orchestrator dependency is narrowed via the CoordinatorContext
// interface declared in this package. It also never imports
// internal/workforce/queue (substrate separation).
//
// Time access routes exclusively through clock.Clock (Q14 C) so the
// time-accelerated test tier drives cadence deterministically;
// direct calls to time.Now / time.NewTicker are forbidden inside this
// package and a vet-style lint enforces.
//
// # References
//
// - Spec §1 Q5 C / Q6 D / Q14 C
// - Spec §5.4.6 (doctrine cadence matrix)
// - ADR-0005 (HRA cadence definition)
//
// # rollout
//
// H-1 (this task) — coordinator skeleton: struct + per-layer
// subscriber registration + Run lifecycle.
// H-2..H-4 — per-layer cadence goroutines (tactical /
// strategic / architectural).
// H-5 — aggregation.WindowOf + 3 aggregators.
// H-6..H-7 — escalation rules + L3 deadlock detection.
// H-8 — phase-boundary trigger OnPhaseBoundary.
package hra
