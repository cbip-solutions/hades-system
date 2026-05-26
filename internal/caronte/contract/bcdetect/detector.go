// SPDX-License-Identifier: MIT
package bcdetect

import "context"

// Detector is the C-7 frozen interface every per-kind detector implements.
// DetectorID returns one of the four CHECK-constrained strings per master
// C-2 schema: "oasdiff", "buf", "gqlparser", or "node-graphql-inspector".
// Detect classifies the diff between oldSpec and newSpec; returns one
// DiffResult per finding (a single contract change may surface multiple
// findings, e.g., a renamed field counts as both removal + addition).
//
// Implementations MUST NOT shell out to external binaries — the SOLE
// sanctioned process spawn in Plan 20 is graphql/nodefallback.go (inv-zen-
// 251). A detector that needs an external tool MUST extend the
// inv-zen-272 sovereignty perimeter via an explicit operator decision +
// ADR; the inv-zen-272 compliance test AST-scans bcdetect/ for os/exec
// imports and asserts exactly one file.
//
// Implementations MUST be safe to call concurrently from multiple
// goroutines on a shared instance — Pipeline.Fan may run multiple
// detectors in parallel across endpoints (D8 future work). Stateless
// wrappers around the Go SDKs satisfy this naturally.
type Detector interface {
	DetectorID() string
	Detect(ctx context.Context, oldSpec, newSpec []byte) ([]DiffResult, error)
}
