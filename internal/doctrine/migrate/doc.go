// SPDX-License-Identifier: MIT
// Package migrate dispatches schema-version migrations for doctrine TOML payloads.
//
// Per Q15 A (in-memory only schema migration with operator-confirmed write-back),
// the migrate package OPERATES ON RAW BYTES + RETURNS *v1.Schema ONLY. It NEVER
// writes to the filesystem. Operator-explicit write-back lives in the CLI
// (`zen doctrine migrate <path> --confirm`), which calls MigrateChain THEN performs
// the WriteFile separately. Splitting these concerns preserves invariant:
// "Schema migration write-back is operator-explicit; daemon never auto-rewrites
// disk files".
//
// Per invariant (Doctrine schema_version on disk monotonically non-decreasing),
// the dispatcher rejects downgrade attempts via ErrSchemaVersionDowngradeRejected.
// Concretely if the proposed target schema_version is lower than the source's
// schema_version (e.g., a hypothetical future MigrateV2ToV1), the dispatcher
// refuses to invoke the migrator and returns the sentinel error. The CLI surfaces
// this to the operator with a clear "downgrade rejected" message.
//
// Per invariant (internal/doctrine/* ⊥ internal/store), this package never
// imports internal/store. Lint-enforced via noStoreImportAnalyzer.
//
// Chain semantics:
//
// - Source version == CurrentSchemaVersion → passthrough Migrator (parse + return)
// - Source version == CurrentSchemaVersion - 1 minor → V→V+1 Migrator
// - Source version older than current-1 → ErrSchemaVersionTooOld
// (operator must run `zen doctrine migrate <path>` chained migrations
// manually, or fall back to a previous binary version)
// - Source version newer than current → ErrSchemaVersionUnsupported
// (operator likely hand-edited a future-schema file or installed wrong binary)
//
// ships:
//
// - MigrateChain dispatcher + chain registry + passthrough Migrator
// - V1→V2 placeholder (deliberate ErrMigrationNotImplemented; first real
// migration ships when schema bumps in release+)
//
// Trust-tier delegation note:
// The passthrough Migrator parses with parser.ParseOpts{AllowTransverseDeclaration:
// true} because the migrate package is trust-tier-agnostic by design — the
// Migrator function signature is `func(data []byte) (*v1.Schema, error)` and
// does not carry trust-tier information. Callers that source data from
// non-trusted tiers
// MUST enforce invariant transverse-rejection separately, either by calling
// parser.ParseStrict with AllowTransverseDeclaration=false BEFORE invoking
// MigrateChain (and only invoking MigrateChain on the raw bytes after the
// strict parse succeeds), OR by re-parsing the migrated *v1.Schema's source
// bytes with strict opts. This separation keeps the migrate package shape
// minimal at the cost of one redundant pre-flight parse on the user-tier
// reload path; the alternative (threading opts through MigrateChain) bloats
// every Migrator's signature for a concern that lives at the parser layer.
//
// References spec §1 Q15 A; §2.1 migrate package perimeter; §3.7 schema migration
// flow; §4.3 sentinel error inventory; §7.2 invariant + invariant.
package migrate
