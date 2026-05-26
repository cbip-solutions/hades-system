// SPDX-License-Identifier: MIT
// Package augment ships the 5-lane RRF augmentation pipeline that turns
// operator prompts into doctrine-aware, privacy-filtered, budget-gated,
// audit-anchored, cache-split context bundles consumed by Hermes via the
// daemon's /v1/augment endpoint.
//
// See types.go for the package overview comment with full Q-decision /
// invariant / boundary documentation. This file exists so `go doc
// github.com/cbip-solutions/hades-system/internal/augment` reports the package
// docstring even when no other Go file is loaded.
//
// Phase ownership (per docs/superpowers/plans/2026-05-10-plan-11-phase-C-*):
//   - C-1 (this file + types.go + sentinel.go): types + sentinels + doc
//   - C-2 (doctrine_gate.go): DoctrineGate.Check
//   - C-3 (budget_gate.go): BudgetGate.Check + Commit
//   - C-4 (privacy.go): PrivacyFilter.FilterCrossProject
//   - C-5 (aggregator_consumer.go): AggregatorConsumer.Lane{2,4,5}*
//   - C-6 (pipeline.go): Pipeline.Run (5-lane orchestration)
//   - C-7 (community_summarize.go): structural cluster summarization
//   - C-8 (cache_split.go): static/volatile split
//   - C-9 (truncation.go): graceful_truncate guard
//   - C-10 (audit_anchor.go): Plan 9 Tessera leaf emission
package augment
