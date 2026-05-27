// SPDX-License-Identifier: MIT
// Package onboard implements release's shared onboarding infrastructure.
//
// Per spec §0.1 + §3.3: `zen config init` (WizardKindGlobal),
// `zen new` (WizardKindGreenfield), and `zen init` (WizardKindBrownfield)
// all consume the same Wizard engine (internal/onboard/qna/) +
// defaults (internal/onboard/defaults.go) + persisted prefs
// (internal/onboard/prefs/) + preflight checks
// (internal/onboard/preflight/) + reviewed MCP set
// (internal/onboard/mcp/) + Hermes plugin location resolver
// (internal/onboard/plugin/).
//
// Per spec §3.4 + invariant: this package and its subpackages NEVER
// import internal/store. Audit emits via internal/audit/chain/ (no
// store dep) or daemon HTTP POST /v1/events.
//
// Per spec §2.3 Q3=D hybrid wizard UX: Q0 three-way prompt at engine
// entry (Recommended / Reuse / Customize); Path 1 + Path 2 ask 0
// follow-up Qs; Path 3 uses sequential narrowing (gh auth login
// pattern per SOTA-2 #8) via bubbletea router-model.
//
// # Invariants enforced by this package + subpackages
//
// - invariant — boundary discipline: NEVER import internal/store.
// - invariant — Hermes Agent ≥0.13.0 (preflight/hermes.go).
// - invariant — plugin format remnants halt (preflight/plugin_format.go).
// - invariant — smart-default confidence ≥0.6 (mcp/smart_default.go).
// - invariant — XDG-canonical path convention (plugin/xdg.go).
// - invariant — cross-platform path tests gate (test infra).
// - invariant — plugin location resolved at install (plugin/location.go).
package onboard
