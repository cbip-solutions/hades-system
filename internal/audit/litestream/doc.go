// SPDX-License-Identifier: MIT
// Package litestream owns the per-project Litestream subprocess
// lifecycle, S3 prefix configuration, Tessera POSIX rsync scheduler,
// and month-end cold archive worker that together implement spec
// §1 Q5 (max-scope: continuous WAL + nightly Tessera rsync + month-end
// partition seal cold archive; default: hourly checkpoint + weekly +
// month-end; capa-firewall: continuous + nightly + month-end on
// object-lock immutable bucket).
//
// invariant: this package MUST NOT import internal/store. All
// chain-state queries + partition-seal updates flow through
// internal/daemon/auditadapter, which
// is the sole invariant bridge.
//
// invariant is moot here (no LLM traffic).
//
// invariant (per-project Tessera tile-log isolation): Manager keys
// every operation by project_id; cross-project method calls are
// rejected at the type level via the ProjectID-bearing constructors
// in tessera.Adapter.
//
// Boundary diagram:
//
// ┌──────────────────────────────────────────────────────────────────┐
// │ Daemon main (cmd/zen-swarm-ctld/main.go) │
// │ │ wires litestream.Manager + tessera.Adapter │
// │ ▼ │
// │ litestream.Manager │
// │ ├─ litestream.subprocess goroutine (per project, C-1+C-4) │
// │ ├─ tessera.rsync_scheduler goroutine (per doctrine, C-5) │
// │ └─ cold_archive.worker goroutine (month-end seal trigger, C-6) │
// │ │ subscribes `audit.partition_sealed` events │
// │ ▼ │
// │ auditadapter.PartitionSealStore │
// │ └─ writes cold_archive_url + cold_archive_content_hash │
// │ back into audit_partition_seals row │
// └──────────────────────────────────────────────────────────────────┘
package litestream
