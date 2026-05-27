// SPDX-License-Identifier: MIT
// Package migrate implements `hades migrate claude-code`: import an existing
// Claude Code installation (~/.claude/) into Hermes plugin format + hades
// doctrine TOML + Hermes config equivalents.
//
// Sub-packages:
// - source/ read-only walkers for ~/.claude/ surfaces
// - mapping/ source-to-target mapping table
// - writer/ Hermes/hades target writers (atomic + backup)
// - golden/ regression fixtures
//
// Boundary (inv-hades-031): this package NEVER imports internal/store. Audit
// events emit via internal/audit/chain/. Filesystem mutations only happen in
// writer/; source/ and mapping/ are pure functions.
//
// Invariants implemented in this phase:
// - inv-hades-177 (backup-before-modify; writer/backup.go)
// - inv-hades-183 (CC permissions 1:1 preservation; writer/doctrine_toml.go)
// - inv-hades-185 (hades migrate --help grouped;../cli/migrate.go)
package migrate
