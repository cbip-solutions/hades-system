// SPDX-License-Identifier: MIT
// Package adr implements the Structured MADR (Markdown Architecture
// Decision Record) machine-readable index spec §1 Q7 A.
//
// The package owns the ADR frontmatter contract (defined in
// architecture records — JSON Schema Draft-07), parses YAML
// frontmatter from markdown ADRs, validates against the schema +
// repo-wide ID uniqueness + supersede cycle detection, emits the dual
// JSON manifest (_index.json flat + _graph.json supersede + relates
// edges) consumed by `make verify-invariants` per invariant,
// migrates the existing 39 markdown-headers-only ADRs to Structured
// MADR (one-time tool), and exposes the 5 ADR transition events
// (proposed / accepted / rejected / superseded / deprecated) for
// downstream chain anchoring.
//
// # Boundary
//
// This package MUST NOT import internal/store directly. Chain
// integration flows through the EventSink interface declared here +
// the adapter implementation in internal/daemon/auditadapter/ (Phase
// B). The bridge translates adr.* event values into the canonical
// eventlog.* typed payloads and calls eventlog.Append.
//
// This package MAY import internal/orchestrator/eventlog ONLY for
// the typed payload structs (no behaviour coupling) — the structs
// are wire-format definitions shared with the chain package, so
// keeping them canonical avoids drift across phases.
//
// # Invariants
//
// - invariant: ADR id is unique repo-wide. The validator enforces
// this; the `zen adr index --check` command + `make
// verify-invariants` Makefile target make the check load-bearing
// in CI. Two ADRs claiming the same id fail the gate with a
// clear error pointing at both source files.
//
// - invariant: dual JSON manifest (_index.json + _graph.json)
// must be regenerated after any ADR add/transition. The CLI
// command `zen adr index --check` regenerates and diffs against
// the on-disk manifests; non-empty diff fails the gate.
//
// - invariant: this package never imports internal/store; chain
// integration via internal/daemon/auditadapter/.
//
// # State machine
//
// ADR status transitions are governed by a minimal state machine
// (see internal/adr/transitions.go):
//
// proposed → accepted, rejected
// accepted → superseded, deprecated
// rejected/superseded/deprecated → terminal
// Reserved is a file-creation-time-only marker; transitions API
// rejects any move involving Reserved.
//
// The state machine is captured in three places (Go constants, JSON
// Schema enum, transitions.IsValid) per doctrine
// domain-invariants-load-bearing — three places to catch one drift.
//
// # Migration tool
//
// The 39 existing ADRs (0001-0008 + 0030-0038) ship with markdown
// headers (`**Status**: Accepted`, `**Date**: 2026-04-30`,...).
// migrate.go parses these via regex, emits Structured
// MADR YAML frontmatter at the top, removes the now-redundant
// markdown headers (preserving body verbatim), and produces a
// MigrationReport for operator review before the bulk migration
// commit. The migration is one-time and idempotent — re-running on
// already-migrated files produces no-op output.
package adr
