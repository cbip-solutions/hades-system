// SPDX-License-Identifier: MIT
// Package federation owns the per-daemon workspace.db: the C-2 cross-repo
// API-contract federation schema (caronte_workspaces +
// caronte_workspace_members + contract_links + breaking_changes +
// breaking_change_consumers) and the CRUD surface release's later phases
// (linker, breaking-change detector, L10 coordinator, MCP surface, TUI)
// write through.
//
// Storage location: the workspace.db lives at the daemon
// state path — $ZEN_STATE_DIR/zen-swarm/workspace.db if set, else
// $XDG_STATE_HOME/zen-swarm/workspace.db (Linux), else
// $HOME/Library/Application Support/zen-swarm/workspace.db (macOS), else
// $HOME/.local/state/zen-swarm/workspace.db (POSIX fallback). One DB per
// daemon process (workspace federation is workspace-scoped, not
// project-scoped — invariant separation: per-project caronte.db lives at
// <canonical>/.zen/caronte.db, this is the orthogonal cross-project
// federation tier).
//
// Boundary: this package and ALL its callers under
// internal/caronte/contract/{extract,link,break} + internal/caronte/coordinated
// NEVER import internal/store. The ONLY internal/caronte/... import this
// package itself makes is internal/caronte/store (for the FROZEN value types
// ContractLink + WorkspacePolicy + the sentinel error set release M ships).
// The compliance test
// tests/compliance/inv_zen_271_boundary_no_internal_store_test.go enforces
// the prohibition; federationBoundarySentinel (db.go) is the runtime witness.
//
// Audit: every workspace-level write
// (contract_links INSERT, breaking_changes INSERT, coordinated-fix dispatch,
// federated-query denied-access, workspace-policy mutation) emits a release
// Tessera audit row via the single C-11 chokepoint EmitAudit(ctx,
// *tessera.Adapter, Event) — append-only, hash-chained. Callers MUST NOT
// call tessera.Adapter.AppendLeaf directly; the AST scan in
// tests/compliance/inv_zen_269_audit_every_write_test.go asserts EmitAudit
// is the only release AppendLeaf call site. The FIX-5 AuditEmitter
// interface + NewAuditEmitter(adapter, workspaceID) constructor provide
// the per-workspace adapter + wire into the consumers.
//
// Capa-firewall extension: every persistent contract_links /
// breaking_changes write transits store.Workspace.authorize() (release M's
// chokepoint at workspace.go:117). The seam swap in
// internal/caronte/store/workspace.go (Task A-12) routes through this
// package's LinkStore port AFTER the existing authorize() gate — never
// before — so the capa-firewall contract M extends to release
// persistence without change.
//
// CGO split: the schema + the file-opening core lives in cgo-tagged files
// (mattn/sqlite3 only links under CGO). The package's CGO-disabled variant
// returns ErrCGODisabled at Open/Init/CRUD entry points so the daemon
// cross-compiles (GOOS=linux CGO_ENABLED=0 go build./...) and degrades
// gracefully (no workspace federation; the engine surfaces degraded_mode).
//
// invariant: this package makes NO web calls; all SQLite + Tessera I/O is
// local.
package federation
