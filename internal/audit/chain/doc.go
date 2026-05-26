// SPDX-License-Identifier: MIT
// Package chain implements the Plan 9 Phase B audit_events_raw chain
// integrity layer: per-record sha256 hash chain (prev_hash → record_hash)
// over the existing audit_events_raw table, plus monthly partition seal
// computation, plus a chain walker for verify-chain semantics, plus a
// one-time backfill walker for post-migration data.
//
// This package is the pure-Go domain layer for chain integrity. It
// NEVER imports internal/store — boundary inv-zen-031 enforced. All
// store-side I/O goes through the EventStore interface (store.go),
// which is satisfied by internal/daemon/auditadapter (the Plan 9
// equivalent of bypassadapter / dispatcheradapter / orchestratoradapter
// from earlier plans).
//
// Chain hash algorithm (Q3 C, spec §1.Q3 + §2.4 migration 059):
//
//	record_hash = sha256(
//	    prev_hash || "|" || event_type || "|" || payload || "|" || ts
//	)
//
// where:
//   - prev_hash is the previous row's record_hash (hex), or "" for genesis
//   - event_type is the audit_events_raw.type column
//   - payload is the audit_events_raw.payload_json column (raw bytes)
//   - ts is the audit_events_raw.emitted_at column (unix seconds, int64)
//   - "|" (byte 0x7C) is a deterministic field separator (see compute.go
//     for adversarial-construction rationale)
//
// Partition derivation (spec §1.Q3 line 199):
//
//	partition_id = strftime("%Y_%m", emitted_at, "unixepoch")
//
// Implemented in Go via time.Unix(emitted_at, 0).UTC().Format("2006_01")
// (the magic 2006-01-02 reference time, "01" → numeric month). UTC is
// load-bearing for cross-host determinism.
//
// Per-event Tessera leaf: not directly handled in this package;
// auditadapter.Adapter (B-9 + B-10) dispatches the (event_id,
// payload_hash, record_hash) tuple to internal/audit/tessera (Phase A)
// for batched leaf storage. The tessera_leaf_id column is updated
// post-batch-flush via store.UpdateTesseraLeafID (auditadapter).
//
// Per-partition seal (Q3 C hybrid): SealPartition computes the partition
// final_record_hash + appends a seal-typed Tessera leaf + records
// daemon witness signature in audit_partition_seals. Called by the
// monthly partition.seal_worker goroutine (auditadapter, Phase J wires
// the goroutine; Phase B ships the SealPartition function pure-domain).
//
// Errors
//
//	ErrChainTampered     — record_hash mismatch during walk
//	ErrChainGap          — missing prev_hash chain (rowid skip mid-partition)
//	ErrPartitionSealMissing — partition has no seal but month is closed
//	ErrChainStoreClosed  — EventStore reports the underlying store is closed
//	ErrInvalidPrevHash   — prev_hash is neither "" nor 64-char hex
//	ErrEmptyEventType    — event_type is empty (rejected by Compute)
//	ErrInvalidTimestamp  — ts <= 0 (matches migration 055 CHECK)
package chain
