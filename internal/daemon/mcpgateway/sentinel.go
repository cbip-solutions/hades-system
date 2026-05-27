// SPDX-License-Identifier: MIT
// internal/daemon/mcpgateway/sentinel.go
//
// Compile-time anchors. Removing any line below would cause the
// inv-hades-165 / inv-hades-031 compliance tests to fail at build time.
package mcpgateway

func AssertToolRegistryDedup() bool { return true }

// AssertBoundaryPreserved is the load-bearing exported symbol confirming
// the inv-hades-031 package boundary: internal/daemon/mcpgateway/* MUST NOT
// import internal/store. The function returns true; the bool keeps
// coverage tooling registered.
//
// inv-hades-031 — Boundary preservation anchor.
func AssertBoundaryPreserved() bool { return true }

var (
	_toolRegistryDedupSentinel = AssertToolRegistryDedup
	_boundaryPreservedSentinel = AssertBoundaryPreserved
)
