// SPDX-License-Identifier: MIT
package spikes

type Severity int

const (
	SeverityOK Severity = iota

	SeverityLow

	SeverityMedium

	SeverityHigh

	SeverityCatastrophic
)

func (s Severity) String() string {
	switch s {
	case SeverityOK:
		return "OK"
	case SeverityLow:
		return "LOW"
	case SeverityMedium:
		return "MEDIUM"
	case SeverityHigh:
		return "HIGH"
	case SeverityCatastrophic:
		return "CATASTROPHIC"
	default:
		return "UNKNOWN"
	}
}

func ParseSeverity(s string) Severity {
	switch s {
	case "OK":
		return SeverityOK
	case "LOW":
		return SeverityLow
	case "MEDIUM":
		return SeverityMedium
	case "HIGH":
		return SeverityHigh
	case "CATASTROPHIC":
		return SeverityCatastrophic
	default:
		return SeverityOK
	}
}
