// SPDX-License-Identifier: MIT
// Package auditadapter bridges *store.Store to the release chain layer
// (internal/audit/chain). Pattern matches bypassadapter,
// dispatcheradapter, orchestratoradapter,
// projectctxadapter.
//
// Boundary (inv-hades-031): the chain layer (internal/audit/chain) NEVER
// imports internal/store. Adapter satisfies chain.EventStore via
// field-by-field copy between chain.* value types and store.* row
// types — same shape, deliberate layer boundary.
//
// Chain compute architecture (B-4 decision): chain hashes are computed
// in app-layer post-INSERT, NOT in a SQLite recursive trigger. The
// orchestratoradapter inserts the raw audit_events_raw row;
// auditadapter.OnEmitRaw is called immediately after to compute
// (prev_hash, record_hash, partition_id) and UPDATE the same row via
// store.UpdateChainColumns. The migration 059 REFUSE triggers permit
// this UPDATE because the WHEN clause checks pre-image emptiness
// (one-time ” → non-empty transition).
//
// (this package) ships:
// - Adapter satisfies chain.EventStore
// - OnEmitRaw — chain compute coordinator (Plan H wires post-INSERT)
// - OnTesseraBatchFlushed — sets tessera_leaf_id once batch worker
// reports the assigned leaf id
//
// extends with Tessera dispatch (per-event leaf integration);
// wires auditadapter into orchestratoradapter's audit emit hot
// path; wires the partition.seal_worker goroutine + tamper
// response dispatcher.
//
// Boot-time ordering contract:
//
// 1. store.Open + Migrate (existing daemon boot; cmd/hades-ctld/main.go)
// 2. auditadapter.New(store) constructs the chain compute coordinator
// 3. chain.Backfill runs synchronously (cmd/hades-ctld/main.go
// bootBackfillChain helper) — chain-links any historical
// audit_events_raw rows from pre- sessions (release/8 events
// that landed with empty chain columns)
// 4. wires Adapter.OnEmitRaw into the orchestratoradapter
// audit emit hot path — new rows are chain-linked at insert time
// 5. Daemon starts accepting requests
//
// This ordering is load-bearing: a new row INSERT before Backfill
// completes risks racing the chain tip read; a new emit before
// OnEmitRaw is wired persists with empty chain columns (recoverable
// by next Backfill but adds doctor-noise in the meantime).
//
// ships steps 1-3 wiring (cmd/hades-ctld/main.go boot-time
// chain.Backfill call via bootBackfillChain). ships step 4
// (orchestratoradapter OnEmitRaw integration). Until lands,
// new audit_events_raw rows persist with empty chain columns; the next
// daemon restart chain-links them via Backfill and
// the release doctor surface (audit.chain-integrity) reports the
// transient gap until reboot.
//
// CANONICAL Adapter struct shape (FINAL at B-9 per review
// CRITICAL-11):
//
// type Adapter struct {
// s *store.Store // B-9 wiring (required)
// tessera TesseraAdapter // B-10 wires; nil at B-9 commit
// s3 S3Client // wires; nil at B-9 commit
// litestream LitestreamMgr // wires; nil at B-9 commit
// coldArchive ColdArchiver // wires; nil at B-9 commit
// }
//
// Subsequent phases (B-10, C, H) only ADD methods; never new fields.
// The four dependency interfaces are declared in options.go (B-9
// forward-compat seams).
package auditadapter
