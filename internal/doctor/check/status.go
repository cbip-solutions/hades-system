// SPDX-License-Identifier: MIT
// Package check — status.go ships the Status + FixMode enums declared in
// the HADES design spec §3.3.
//
// Status (4 levels per design choice+):
// - StatusPass (0) — check satisfied; nothing to remediate
// - StatusWarn (1) — check soft-failed; degraded operation; operator-actionable
// - StatusFail (2) — check hard-failed; blocking; remediation required
// - StatusSkip (3) — check unable to run (precondition missing; ctx cancelled;
// subsystem opt-out — bypass-config not extracted)
//
// FixMode (4 levels per design choice+):
// - FixModeReadOnly — print fix suggestion (default behavior; no execution)
// - FixModeInteractive — execute with per-check `[y/N]` prompt
// - FixModeAutoSafe — execute idempotent ops only (skip destructive)
// - FixModeYes — skip all prompts (CI use; requires explicit operator authz)
package check

type Status int

const (
	StatusPass Status = iota

	StatusWarn

	StatusFail

	StatusSkip
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	case StatusSkip:
		return "skip"
	default:
		return "unknown"
	}
}

func (s Status) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *Status) UnmarshalJSON(data []byte) error {

	if len(data) >= 2 && data[0] == '"' && data[len(data)-1] == '"' {
		data = data[1 : len(data)-1]
	}
	switch string(data) {
	case "pass":
		*s = StatusPass
	case "warn":
		*s = StatusWarn
	case "fail":
		*s = StatusFail
	case "skip":
		*s = StatusSkip
	default:
		*s = StatusSkip
	}
	return nil
}

func (s Status) Glyph(ascii bool) string {
	if ascii {
		switch s {
		case StatusPass:
			return "OK"
		case StatusWarn:
			return "WARN"
		case StatusFail:
			return "FAIL"
		case StatusSkip:
			return "SKIP"
		default:
			return "??"
		}
	}
	switch s {
	case StatusPass:
		return "✓"
	case StatusWarn:
		return "⚠"
	case StatusFail:
		return "✗"
	case StatusSkip:
		return "⊝"
	default:
		return "?"
	}
}

type FixMode int

const (
	// FixModeReadOnly — print fix command/suggestion; do NOT execute.
	// Default mode when `--fix` is absent.
	FixModeReadOnly FixMode = iota

	FixModeInteractive
	// FixModeAutoSafe — execute Fix() only when IsDestructive() == false.
	// Destructive checks fall back to FixModeReadOnly + warning.
	FixModeAutoSafe

	FixModeYes
)

func (f FixMode) String() string {
	switch f {
	case FixModeReadOnly:
		return "read-only"
	case FixModeInteractive:
		return "interactive"
	case FixModeAutoSafe:
		return "auto-safe"
	case FixModeYes:
		return "yes"
	default:
		return "unknown"
	}
}
