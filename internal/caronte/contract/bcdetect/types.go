// SPDX-License-Identifier: MIT
// Package bcdetect — Plan 20 breaking-change detection (master C-7).
//
// Owns the Detector interface, Severity enum, DiffResult value type, and
// BreakingEvent (the cross-package event Phase H's L10 Coordinator consumes).
// Per-kind detector implementations live in subpackages (openapi/, proto/,
// graphql/) so the imports scan in inv-zen-267 can assert exactly one Go diff
// library per subpackage.
//
// Boundary inv-zen-271: this package MUST NOT import internal/store; it
// bridges only via internal/caronte/store + internal/caronte/store/federation
// (the Plan 20 Phase A boundary).
//
// Package name discipline (Stage-2 CRITICAL-2 + AS-BUILT CORRECTION #6 in
// the master): the directory is `bcdetect/` (not `break/` — Go reserved
// keyword). All public surface uses the `bcdetect.X` qualifier.
//
// References spec §7 (L9 breaking-change); master C-7 (Detector interface);
// master C-11 (Tessera audit emission); inv-zen-267 (canonical tools); inv-
// zen-251 (Node fallback sovereignty extension).
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
