// SPDX-License-Identifier: MIT
// Package litestream owns the per-project Litestream subprocess
// lifecycle, S3 prefix configuration, Tessera POSIX rsync scheduler,
// and month-end cold archive worker that together implement spec
// §1 Q5 (max-scope: continuous WAL + nightly Tessera rsync + month-end
// partition seal cold archive; default: hourly checkpoint + weekly +
// month-end; capa-firewall: continuous + nightly + month-end on
// object-lock immutable bucket).
//
// inv-zen-031: this package MUST NOT import internal/store. All
// chain-state queries + partition-seal updates flow through
// internal/daemon/auditadapter (Phase B + Phase C extensions), which
// is the sole inv-zen-031 bridge.
//
// inv-zen-088 is moot here (no LLM traffic).
//
// inv-zen-144 (per-project Tessera tile-log isolation): Manager keys
// every operation by project_id; cross-project method calls are
// rejected at the type level via the ProjectID-bearing constructors
// in tessera.Adapter (Phase A).
//
// Boundary diagram (Phase C scope):
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│ Daemon main (cmd/zen-swarm-ctld/main.go)                         │
//	│   │ wires litestream.Manager + tessera.Adapter (Phase A)         │
//	│   ▼                                                              │
//	│ litestream.Manager                                               │
//	│   ├─ litestream.subprocess goroutine (per project, C-1+C-4)      │
//	│   ├─ tessera.rsync_scheduler goroutine (per doctrine, C-5)       │
//	│   └─ cold_archive.worker goroutine (month-end seal trigger, C-6) │
//	│        │ subscribes Phase B `audit.partition_sealed` events      │
//	│        ▼                                                         │
//	│ auditadapter.PartitionSealStore (Phase C extension of Phase B)   │
//	│   └─ writes cold_archive_url + cold_archive_content_hash         │
//	│      back into audit_partition_seals row (inv-zen-031 bridge)    │
//	└──────────────────────────────────────────────────────────────────┘
package litestream
