// SPDX-License-Identifier: MIT
// Package check — semver.go ships the canonical semver helper shared by
// internal/doctor/hermes (InstallCheck binary-version comparison) and
// internal/doctor/mcp (AvailabilityCheck pin comparison).
//
// Pre-canonical state (Plan 13 Phase F2 review surfaced as F-imp-3): each
// consumer shipped its own isSemverLike / compareSemver / versionLessThan
// / atoi helpers. The two atoi implementations had DIFFERENT semantics on
// non-digit input (hermes assumed pre-validated digits-only; mcp parsed
// digits-until-first-non-digit). Duplicate logic + behavioural mismatch
// violated no-tech-debt + risked downstream bugs.
//
// Boundary (inv-zen-031): semver.go consumes ONLY the stdlib. The check
// package is the canonical shared substrate for doctor primitives — both
// internal/doctor/hermes and internal/doctor/mcp already import it for
// the Check interface, so consolidation here adds no new boundary
// crossings.
//
// Two parsers + one comparator:
//   - ParseSemver: STRICT — requires exactly 3 dotted numeric components.
//     Rejects pre-release tags (`1.2.3-pre` → error), excess components
//     (`1.2.3.4` → error), missing components (`1.2` → error), and any
//     non-digit content. Used by hermes for binary-version probe (Plan
//     13 requires Hermes ≥0.13.0; the strict shape avoids accidental
//     comparison against pre-release snapshots).
//   - ParseSemverLax: PERMISSIVE — parses up to 3 components, each via
//     digit-until-first-non-digit (matches the legacy mcp.atoi semantic
//     used for curated MCP catalog version-pin comparison; tolerates
//     trailing `-pre` tags on pinned packages).
//   - CompareVersions: -1/0/1 canonical comparison; lex order over
//     (Major, Minor, Patch).
package check

import (
	"errors"
	"strings"
)

type Version struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string
}

var ErrInvalidSemver = errors.New("check: invalid semver string")

func ParseSemver(s string) (Version, error) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, errInvalidSemver(s)
	}
	out := Version{}
	for i, p := range parts {
		if p == "" {
			return Version{}, errInvalidSemver(s)
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return Version{}, errInvalidSemver(s)
			}
		}
		n := parseDigits(p)
		switch i {
		case 0:
			out.Major = n
		case 1:
			out.Minor = n
		case 2:
			out.Patch = n
		}
	}
	return out, nil
}

func ParseSemverLax(s string) Version {
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	out := Version{}
	limit := len(parts)
	if limit > 3 {
		limit = 3
	}
	for i := 0; i < limit; i++ {
		n, suffix := parseDigitsWithRemainder(parts[i])
		switch i {
		case 0:
			out.Major = n
		case 1:
			out.Minor = n
		case 2:
			out.Patch = n
			out.PreRelease = suffix
		}
	}
	return out
}

// CompareVersions returns -1 if a < b, 0 if a == b, 1 if a > b. Compares
// (Major, Minor, Patch) lexicographically; PreRelease is IGNORED for
// ordering (matches legacy mcp.versionLessThan: `1.2.3-pre` orders
// equal to `1.2.3`). Callers that need pre-release-aware ordering MUST
// build their own comparison on top of Version.
func CompareVersions(a, b Version) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// parseDigits parses a digits-only string into an int. Caller MUST have
// pre-validated that every rune is '0'..'9'. Used by ParseSemver after
// the strict-validation loop.
func parseDigits(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func parseDigitsWithRemainder(s string) (int, string) {
	n := 0
	idx := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		idx++
	}
	if idx >= len(s) {
		return n, ""
	}
	return n, s[idx:]
}

func errInvalidSemver(input string) error {
	return &invalidSemverErr{input: input}
}

type invalidSemverErr struct {
	input string
}

func (e *invalidSemverErr) Error() string {
	return ErrInvalidSemver.Error() + ": " + e.input
}

func (e *invalidSemverErr) Is(target error) bool {
	return target == ErrInvalidSemver
}

func (e *invalidSemverErr) Unwrap() error { return ErrInvalidSemver }
