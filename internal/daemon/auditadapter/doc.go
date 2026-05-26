// SPDX-License-Identifier: MIT
// Package auditadapter bridges *store.Store to the Plan 9 chain layer
// (internal/audit/chain). Pattern matches bypassadapter (Plan 2),
// dispatcheradapter (Plan 3), orchestratoradapter (Plan 5),
// projectctxadapter (Plan 7).
//
// Boundary (inv-zen-031): the chain layer (internal/audit/chain) NEVER
// imports internal/store. Adapter satisfies chain.EventStore via
// field-by-field copy between chain.* value types and store.* row
// types — same shape, deliberate layer boundary.
//
// Chain compute architecture (B-4 decision): chain hashes are computed
// in app-layer post-INSERT, NOT in a SQLite recursive trigger. The
// orchestratoradapter (Plan 5) inserts the raw audit_events_raw row;
// auditadapter.OnEmitRaw is called immediately after to compute
// (prev_hash, record_hash, partition_id) and UPDATE the same row via
// store.UpdateChainColumns. The migration 059 REFUSE triggers permit
// this UPDATE because the WHEN clause checks pre-image emptiness
// (one-time ” → non-empty transition).
//
// Phase B (this package) ships:
//   - Adapter satisfies chain.EventStore
//   - OnEmitRaw      — chain compute coordinator (Plan H wires post-INSERT)
//   - OnTesseraBatchFlushed — sets tessera_leaf_id once batch worker
//     reports the assigned leaf id
//
// Phase B-10 extends with Tessera dispatch (per-event leaf integration);
// Phase H wires auditadapter into orchestratoradapter's audit emit hot
// path; Phase J wires the partition.seal_worker goroutine + tamper
// response dispatcher.
//
// Boot-time ordering contract (Phase H wiring requirement):
//
//  1. store.Open + Migrate (existing daemon boot; cmd/zen-swarm-ctld/main.go)
//  2. auditadapter.New(store) constructs the chain compute coordinator
//  3. chain.Backfill runs synchronously (cmd/zen-swarm-ctld/main.go
//     bootBackfillChain helper) — chain-links any historical
//     audit_events_raw rows from pre-Phase-9 sessions (Plan 5/8 events
//     that landed with empty chain columns)
//  4. Phase H wires Adapter.OnEmitRaw into the orchestratoradapter
//     audit emit hot path — new rows are chain-linked at insert time
//  5. Daemon starts accepting requests
//
// This ordering is load-bearing: a new row INSERT before Backfill
// completes risks racing the chain tip read; a new emit before
// OnEmitRaw is wired persists with empty chain columns (recoverable
// by next Backfill but adds doctor-noise in the meantime).
//
// Phase C ships steps 1-3 wiring (cmd/zen-swarm-ctld/main.go boot-time
// chain.Backfill call via bootBackfillChain). Phase H ships step 4
// (orchestratoradapter OnEmitRaw integration). Until Phase H lands,
// new audit_events_raw rows persist with empty chain columns; the next
// daemon restart chain-links them via Backfill (Phase C boot path) and
// the Plan 9 doctor surface (audit.chain-integrity) reports the
// transient gap until reboot.
//
// CANONICAL Adapter struct shape (FINAL at B-9 per Stage 2 review
// CRITICAL-11):
//
//	type Adapter struct {
//	    s           *store.Store    // B-9 wiring (required)
//	    tessera     TesseraAdapter  // B-10 wires; nil at B-9 commit
//	    s3          S3Client        // Phase H wires; nil at B-9 commit
//	    litestream  LitestreamMgr   // Phase C wires; nil at B-9 commit
//	    coldArchive ColdArchiver    // Phase C wires; nil at B-9 commit
//	}
//
// Subsequent phases (B-10, C, H) only ADD methods; never new fields.
// The four dependency interfaces are declared in options.go (B-9
// forward-compat seams).
package auditadapter
