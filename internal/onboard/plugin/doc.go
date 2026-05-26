// SPDX-License-Identifier: MIT
// Package plugin implements Plan 13's Hermes plugin location
// resolution + installation primitives.
//
// Per spec §2.13 Q13=D + §4.5 Phase A spike re-verify + §8.6
// inv-zen-190: the plugin install location depends on the 13-A0
// Phase 0 spike outcome.
//
//	spike PASS → project-scope primary:
//	  <repo>/.hermes/plugins/zen-swarm/
//
//	spike FAIL → user-scope per-project fallback:
//	  ~/.hermes/plugins/zen-swarm-<slug>/
//	  (ADR-0086 documents fallback rationale; Plan 17b conditional
//	   sibling work triggered)
//
// Per spec §8.6 inv-zen-186: XDG-canonical path convention; macOS
// precedence operator-config. Helpers in xdg.go honor $XDG_CONFIG_HOME
// / $XDG_STATE_HOME / $XDG_CACHE_HOME / $XDG_DATA_HOME with $HOME-based
// fallback.
//
// Per inv-zen-031: this package does NOT import internal/store.
//
// # Surface
//
// The package exposes the following load-bearing symbols (Master plan
// §"Cross-phase type sharing"):
//
//   - Location (struct{Path,Kind}) + LocationKind enum (with Stringer)
//   - ResolveLocation(spikeOutcome bool) (Location, error)
//   - Slug(absPath string) string — deterministic per-project slug
//   - Install(ctx, InstallOptions) (canonical string, err error)
//   - Uninstall(loc Location) error
//   - XDGConfigDir / XDGStateDir / XDGCacheDir / XDGDataDir helpers
//
// # Invariants
//
//   - inv-zen-186 — XDG-canonical path convention (xdg.go).
//   - inv-zen-190 — plugin location resolved at install (location.go).
//   - inv-zen-031 — boundary discipline (no internal/store import).
package plugin
