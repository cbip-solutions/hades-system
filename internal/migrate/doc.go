// SPDX-License-Identifier: MIT
// Package migrate implements `zen migrate claude-code`: import an existing
// Claude Code installation (~/.claude/) into Hermes plugin format + zen
// doctrine TOML + Hermes config equivalents.
//
// Sub-packages:
//   - source/   read-only walkers for ~/.claude/ surfaces
//   - mapping/  source-to-target mapping table (Plan 13 spec §2.4)
//   - writer/   Hermes/zen target writers (atomic + backup)
//   - golden/   regression fixtures
//
// Boundary (inv-zen-031): this package NEVER imports internal/store. Audit
// events emit via internal/audit/chain/. Filesystem mutations only happen in
// writer/; source/ and mapping/ are pure functions.
//
// Invariants implemented in this phase:
//   - inv-zen-177 (backup-before-modify; writer/backup.go)
//   - inv-zen-183 (CC permissions 1:1 preservation; writer/doctrine_toml.go)
//   - inv-zen-185 (zen migrate --help grouped; ../cli/migrate.go)
package migrate
