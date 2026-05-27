// SPDX-License-Identifier: MIT
// Package amendment implements the Q10 C doctrine-amendment flow:
// Proposer (pattern detection + ADR draft) + Applier (validate + atomic
// commit + reload) + Reverter (operator-initiated git revert + reload) +
// CooldownRegistry (per-doctrine suppression) + range.NextAvailableID
// .
//
// # Architecture
//
// The package is event-driven and operator-gated. AmendmentProposer
// subscribes to the event log via the narrow EventEmitter
// interface, maintains sliding-window counters keyed by trigger pattern
// (operator-override class, cost-degradation severity, escalation chain
// length), and on doctrine-tunable threshold breach delegates ADR
// drafting to an injected L4Drafter. The drafted markdown is written to
// architecture records and a DoctrineAmendmentProposed
// event is emitted. Operator acknowledgement (via
// OperatorGate) drives AmendmentApplier.Apply: validate-first
// , capture pre-apply TOML SHA-256, atomic git commit,
// HTTP reload signal, emit DoctrineAmendmentApplied. Any post-commit
// failure triggers a deferred AmendmentReverter.Rollback that restores
// the working tree byte-identical to its pre-Apply state
// . Operator deny moves the ADR to
// architecture records and arms a per-doctrine cooldown timer
// suppressing re-proposal of the same trigger pattern.
//
// # Boundaries
//
// - amendment ⊥ internal/store — no SQLite import. Bridged in
// daemon/orchestratoradapter.
// - amendment ⊥ internal/queue — no queue import.
// - amendment ⊥ workforce — no workforce import.
// - amendment ⊥ internal/orchestrator/hra — Drafter is an interface;
// wires the concrete L4 reviewer.
// - amendment ⊥ internal/doctrine for reload — ReloadSignal is an
// interface; daemon HTTP endpoint wires the real implementation.
//
// The package consumes only narrow interfaces (EventEmitter, L4Drafter,
// RangeAllocator, CooldownView, ReloadSignal, GitRunner) so it remains
// trivially substitutable for tests + replay + future transport changes.
//
// # Invariants enforced here
//
// - invariant — Amendment rollback atomicity. AmendmentApplier.Apply
// is transactional: any post-commit failure triggers a deferred
// rollback that leaves the working tree byte-identical to pre-Apply
// state (TOML SHA-256 hash check).
// - invariant — Doctrine validate before apply. Apply MUST call
// doctrine.ValidateAdditive on the proposed diff BEFORE any git
// operation; failure aborts + emits DoctrineAmendmentSuppressed
// {reason:"validate_failed"}.
// - invariant — ADR range reservation. release reserves
// decisions/0020..0029 (10 slots); range.NextAvailableID returns
// ErrADRRangeExhausted + emits ADRRangeExhausted on overflow.
//
// # Cooldown windows (per doctrine)
//
// - max-scope: 24h
// - default: 72h
// - capa-firewall: 168h (1 week)
//
// These match the sliding-window length used for threshold counting;
// after operator deny, cooldown of the same length suppresses
// re-proposal of the identical trigger pattern.
package amendment
