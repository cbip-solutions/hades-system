// SPDX-License-Identifier: MIT
// Package check declares the HADES design canonical Check interface
// + DiagnosticResult value type consumed by internal/doctor/aggregator
// and internal/doctor/fix.
//
// Task F1 ships the interface contract; existing HADES design per-flag
// checks compose via adapter shims in internal/doctor/check/adapters.go
// .
//
// Boundary (invariant): this package consumes ONLY internal/audit/chain/
// (typed event emit) + internal/doctrine/ (active doctrine accessor) +
// internal/cli (existing ProbeResult/ProbeStatus types via the adapter
// shim); it MUST NOT import internal/store.
//
// review IMPORTANT: the Check interface is the load-bearing
// contract doctor aggregator. Adding a method requires
// updating ALL adapter shims (adapters.go) + every concrete Check impl
// . The
// 6-method set (Name/Category/Description/IsDestructive/Run/Fix) is
// locked-in post-F1; growth requires ADR amendment.
//
// Cross-package conversion warning: doctor/check.Status and
// internal/onboard/preflight.Status share names but DIFFERENT numeric
// values (doctor has StatusPass=0; preflight has StatusUnknown=0). They
// are NOT interconvertible by raw int cast. Bridging between the two
// packages MUST go through an explicit translation table; see
// internal/onboard/preflight/preflight.go:12-16 for the FORBIDDEN-cast
// warning.
package check

import "context"

type Check interface {
	Name() string
	Category() Category
	Description() string
	IsDestructive() bool
	Run(ctx context.Context) DiagnosticResult
	Fix(ctx context.Context, mode FixMode) error
}

type DiagnosticResult struct {
	Name           string `json:"name"`
	Status         Status `json:"status"`
	Message        string `json:"message,omitempty"`
	Detail         string `json:"detail,omitempty"`
	Hint           string `json:"hint,omitempty"`
	DurationMs     int64  `json:"durationMs"`
	AuditEventHash string `json:"auditEventHash,omitempty"`
}

type Category int

const (
	CategoryPreflight Category = iota

	CategoryRuntime

	CategoryConfiguration

	CategoryHints
)

func (c Category) String() string {
	switch c {
	case CategoryPreflight:
		return "preflight"
	case CategoryRuntime:
		return "runtime"
	case CategoryConfiguration:
		return "configuration"
	case CategoryHints:
		return "hints"
	default:
		return "unknown"
	}
}

func Categories() []Category {
	return []Category{CategoryPreflight, CategoryRuntime, CategoryConfiguration, CategoryHints}
}
