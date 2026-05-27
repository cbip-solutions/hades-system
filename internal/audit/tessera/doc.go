// SPDX-License-Identifier: MIT
// Package tessera implements the transparency-log audit substrate.
//
// Per spec §0 + §1 Q1 D: ships RFC 9162 tile-based transparency
// logs (Tessera) as the audit substrate cross-eje desde día 1. Naive
// sha256 hash chains were rejected en Q1 D as future tech debt vs the
// RFC 9162 SOTA (RFC 6962 EOL feb 2026 per Let's Encrypt 2025-08-14).
//
// Per spec §1 Q2 A: each project owns a private Tessera tile-log
// (POSIX backend) en ~/.local/share/zen-swarm/projects/<id>/audit/
// tessera/. Daemon-global witness co-signature (ECDSA P-256, Keychain
// backed) → daemon_global_checkpoint_log (separate Tessera tile-log
// at ~/.local/share/zen-swarm/global/daemon_checkpoint/) holds triples
// (project_id, project_tree_head, daemon_signature). Project content
// never leaks across project boundaries (only tree heads + signatures
// reach the daemon-global log; opaque to observers).
//
// Per spec §1 Q4 B: batch cadence (BatchMaxAge + BatchMaxSize) is
// doctrine-tunable. wires the configuration knob;
// binds the doctrine bundle. Defaults align with `default` doctrine
// (30s / 1000) so config-less callers operate with a coherent posture.
//
// # Invariants enforced by this package
//
// - invariant — this package MUST NOT import internal/store.
// Bridge internal/daemon/auditadapter/ + the chain layer
// in internal/audit/chain/.
// - invariant — per-project Tessera tile-log isolation: cross-project
// reads MUST fail. Compile-checked: NewProjectAdapter requires a
// non-empty project_id; runtime-checked: Adapter API rejects
// cross-project leaf lookups by reference; analyzer-checked:
// internal/lint/noCrossProjectAtTessera.
// - invariant — daemon witness signature mandatory en daemon_global_
// checkpoint_log. CoSigner.Sign returns SignedSTH only; never
// publishes unsigned to the daemon-global tile-log.
// - T1 (audit chain tampering) defense — per-record hash chain is
// responsibility; this package provides the Tessera
// Merkle inclusion proof primitive (Adapter.VerifyMerkleInclusion).
// - T2 (witness key compromise) defense — ECDSA P-256 in Keychain
// (darwin) / file-based 0600 (linux); rotation cadence driven by
// [audit.witness] doctrine knob.
package tessera
