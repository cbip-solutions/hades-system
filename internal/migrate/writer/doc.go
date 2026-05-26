// SPDX-License-Identifier: MIT
// Package writer applies a mapping.Plan to the filesystem. Writers are
// atomic (temp-file + rename); backups are mandatory when --force is passed
// OR target exists + non-empty (inv-zen-177 backup-before-modify).
//
// Per-target writers (write_skill, write_command, write_hook, write_memory,
// write_hermes_config, doctrine_toml) consume PlanEntry.BodyBytes + render
// to a deterministic target shape. plugin.yaml + __init__.py are emitted at
// end-of-Apply with accumulated register_calls.
//
// Boundary (inv-zen-031): this package NEVER imports internal/store. It
// imports only internal/migrate/mapping for the Plan input type.
//
// Coverage target: ≥90% (security-critical; mutates operator's home).
package writer
