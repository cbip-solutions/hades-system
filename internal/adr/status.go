// SPDX-License-Identifier: MIT
package adr

type Status string

const (
	StatusProposed Status = "proposed"

	StatusAccepted Status = "accepted"

	StatusRejected Status = "rejected"

	StatusSuperseded Status = "superseded"

	StatusDeprecated Status = "deprecated"

	StatusReserved Status = "Reserved"
)

// IsValid reports whether s is one of the six documented Status values.
// Returns false for empty string, unknown strings, or case-mismatched variants
// (e.g. "Accepted" is invalid; the canonical form is "accepted").
// Callers that receive a Status from untrusted input (YAML frontmatter, HTTP
// body, CLI flag) MUST call IsValid before acting — ErrUnknownStatus is the
// sentinel to wrap on failure.
func (s Status) IsValid() bool {
	switch s {
	case StatusProposed, StatusAccepted, StatusRejected,
		StatusSuperseded, StatusDeprecated, StatusReserved:
		return true
	default:
		return false
	}
}

func AllStatuses() []Status {
	return []Status{
		StatusProposed, StatusAccepted, StatusRejected,
		StatusSuperseded, StatusDeprecated, StatusReserved,
	}
}
