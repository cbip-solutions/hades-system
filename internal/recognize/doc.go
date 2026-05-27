// SPDX-License-Identifier: MIT
// Package recognize is the canonical home of release
// `hades recognize` 3-tier signal stack (linguist filters → manifest deps
// → framework configs → monorepo walk → maturity probe) — see
// internal design record
//
// # Forward-declaration status
//
// Task A-5 (internal/onboard/mcp/) imports recognize.Result for
// smart-default detection (C7 reconciliation 2026-05-14 — no shim
// package). To compile, this package forward-declares the canonical
// Result type + dependent evidence types in types.go. will
// extend this package with the detection logic (manifest/, config/,
// monorepo/, maturity/, glob/ subpackages + recognize.go orchestrator)
// per its plan §"Files to create"; the Result type defined here is the
// authoritative cross-phase contract — populates fields rather
// than redefining the struct.
//
// # Cross-phase contract
//
// Tier 1 manifest detectors populate `Result.ManifestDeps` from
// go.mod / package.json / Cargo.toml / requirements.txt / etc.
//
// Tier 2 config detectors populate `Result.EnvVars`,
// `Result.ConfigFiles`, and `Result.Doctrine` from framework-specific
// config files (next.config.{js,ts}, vite.config.*, sentry.config.*,
// .linear.{yml,yaml}, etc.).
//
// `internal/onboard/mcp/smart_default.go` consumes those
// fields directly via Tier 3 `Detected fn` evaluation (inv-hades-179
// confidence ≥0.6 threshold).
//
// # Invariants enforced by this package
//
// - inv-hades-031 — boundary discipline: NEVER import internal/store.
// - inv-hades-179 — smart-default confidence ≥0.6 threshold (enforced
// downstream in internal/onboard/mcp/smart_default.go; this package
// surfaces `Result.PrimaryConfidence` as the threshold input).
package recognize
