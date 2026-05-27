// SPDX-License-Identifier: MIT
// Package bcdetect — release breaking-change detection (master C-7).
//
// Owns the Detector interface, Severity enum, DiffResult value type, and
// BreakingEvent.
// Per-kind detector implementations live in subpackages (openapi/, proto/,
// graphql/) so the imports scan in invariant can assert exactly one Go diff
// library per subpackage.
//
// Boundary invariant: this package MUST NOT import internal/store; it
// bridges only via internal/caronte/store + internal/caronte/store/federation
// .
//
// Package name discipline ( CRITICAL-2 + AS-BUILT CORRECTION #6 in
// the master): the directory is `bcdetect/` (not `break/` — Go reserved
// keyword). All public surface uses the `bcdetect.X` qualifier.
//
// References spec §7 (L9 breaking-change); master C-7 (Detector interface);
// master C-11 (Tessera audit emission); invariant (canonical tools); inv-
// hades-251 (Node fallback sovereignty extension).
package bcdetect

type Severity string

const (
	SevBreaking     Severity = "BREAKING"
	SevDangerous    Severity = "DANGEROUS"
	SevNonBreaking  Severity = "NON_BREAKING"
	SevInsufficient Severity = "INSUFFICIENT"
)

type DiffResult struct {
	DetectorID string
	Kind       string
	Severity   Severity
	Detail     []byte
}

type BreakingEvent struct {
	ChangeID      string
	WorkspaceID   string
	EndpointID    string
	EndpointRepo  string
	Kind          string
	Severity      Severity
	DetectorID    string
	Detail        []byte
	DetectedAt    int64
	ConsumerCount int
}
