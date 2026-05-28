// SPDX-License-Identifier: MIT
// Package plugin implements HADES design's Hermes plugin location
// resolution + installation primitives.
//
// per design contract=D + §4.5 spike re-verify + §8.6
// invariant: the plugin install location depends on the 13-A0
// spike outcome.
//
// spike PASS → project-scope primary:
// <repo>/.hermes/plugins/hades-system/
//
// spike FAIL → user-scope per-project fallback:
// ~/.hermes/plugins/hades-system-<slug>/
// (ADR-0086 documents fallback rationale; HADES design conditional
// sibling work triggered)
//
// per design contract: XDG-canonical path convention; macOS
// precedence operator-config. Helpers in xdg.go honor $XDG_CONFIG_HOME
// / $XDG_STATE_HOME / $XDG_CACHE_HOME / $XDG_DATA_HOME with $HOME-based
// fallback.
//
// Per invariant: this package does NOT import internal/store.
//
// # Surface
//
// The package exposes the following load-bearing symbols (Master plan
// §"Cross-stage type sharing"):
//
// - Location (struct{Path,Kind}) + LocationKind enum (with Stringer)
// - ResolveLocation(spikeOutcome bool) (Location, error)
// - Slug(absPath string) string — deterministic per-project slug
// - Install(ctx, InstallOptions) (canonical string, err error)
// - Uninstall(loc Location) error
// - XDGConfigDir / XDGStateDir / XDGCacheDir / XDGDataDir helpers
//
// # Invariants
//
// - invariant — XDG-canonical path convention (xdg.go).
// - invariant — plugin location resolved at install (location.go).
// - invariant — boundary discipline (no internal/store import).
package plugin
