// SPDX-License-Identifier: MIT
// Package migrate implements `hades migrate claude-code`: import an existing
// Claude Code installation (local agent memory/) into Hermes plugin format + hades
// doctrine TOML + Hermes config equivalents.
//
// Sub-packages:
// - source/ read-only walkers for local agent memory/ surfaces
// - mapping/ source-to-target mapping table
// - writer/ Hermes/hades target writers (atomic + backup)
// - golden/ regression fixtures
//
// Boundary (invariant): this package NEVER imports internal/store. Audit
// events emit via internal/audit/chain/. Filesystem mutations only happen in
// writer/; source/ and mapping/ are pure functions.
//
// Invariants implemented in this stage:
// - invariant (backup-before-modify; writer/backup.go)
// - invariant (CC permissions 1:1 preservation; writer/doctrine_toml.go)
// - invariant (hades migrate --help grouped;../cli/migrate.go)
package migrate
