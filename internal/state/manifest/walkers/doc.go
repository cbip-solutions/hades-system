// SPDX-License-Identifier: MIT
// Package walkers contains the per-section sub-walkers consumed by
// the manifest orchestrator. Each walker is a small + focused +
// independently-testable function/struct producing a typed result
// + a MissingSources slice for failure-mode #12 partial-result
// handling spec §4.1.
//
// inv-hades-031: walkers NEVER import internal/store. Doctrine walker
// receives the registry name list via a callback function; autonomy
// walker reads a filesystem stamp file written by the autonomy
// engine. NO direct DB access.
package walkers
