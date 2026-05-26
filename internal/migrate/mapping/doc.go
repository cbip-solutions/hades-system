// SPDX-License-Identifier: MIT
// Package mapping translates a Claude Code source.Inventory into a Plan that
// the writer applies to the filesystem. The mapping table is canonical per
//
// Pure function: Map(inv, preset) → (Plan, error). Never mutates filesystem.
//
// Preset semantics (spec §2.1):
//   - PresetStrict: halts (ErrUnmappedSurface) on any source kind without a
//     mapping rule. Operator must add a rule before retry. inv-zen-183 demands
//     1:1 preservation for CC permissions; strict mode enforces.
//   - PresetLenient: skips unmapped surfaces + records them in Plan.Warnings.
//     Default for `zen migrate claude-code`.
//
// Boundary (inv-zen-031): this package NEVER imports internal/store. It does
// import internal/migrate/source as a typed-input dependency only.
package mapping
