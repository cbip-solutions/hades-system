// SPDX-License-Identifier: MIT
// Package aggregator — PerProjectKnowledgeStore interface.
//
// Extracted from aggregator.go (D-2) to give the interface a dedicated file
// that mirrors the Embedder extraction pattern (D-7 → embedder_iface.go).
//
// Boundary (inv-hades-031): the shared types and interface are defined in
// internal/knowledge/knowledgetypes (a pure-Go, CGO-free package). This file
// re-exports them as package-level type aliases so existing code inside the
// aggregator package can reference them without the knowledgetypes import path.
//
// Why knowledgetypes instead of defining here: if ProjectHandle, ProjectVault,
// and PerProjectKnowledgeStore live in this (aggregator) package, then any
// code that needs to implement or use the interface (e.g., knowledgeadapter)
// must import this package, which via db.go pulls in mattn/go-sqlite3 (CGO).
// That causes a double-registration panic in any test binary that also imports
// ncruces/go-sqlite3 (from internal/store). knowledgetypes has no CGO
// dependency, so knowledgeadapter can implement the interface without
// importing aggregator at all.
//
// See aggregator.go §"Boundary" package doc comment for the rationale.
package aggregator

import "github.com/cbip-solutions/hades-system/internal/knowledge/knowledgetypes"

type ProjectVault = knowledgetypes.ProjectVault

type PerProjectKnowledgeStore = knowledgetypes.PerProjectKnowledgeStore
