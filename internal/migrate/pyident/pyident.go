// SPDX-License-Identifier: MIT
// Package pyident provides the canonical claude-code → Python identifier
// transformation used across the migrate pipeline. Extracted from
// mapping/frontmatter.go::sanitizePyIdent + writer/write_command.go::
// pyIdentFromCommandName per reviewer I-6 (DRY violation: same logic in
// two places, drift would silently invalidate generated __init__.py vs
// hook/command file names).
//
// Both call sites import this package; a property test in pyident_test.go
// covers the shared invariant (idempotent + deterministic + lex-monotonic
// across the input space).
package pyident

import "strings"

func FromName(s string) string {
	if s == "" {
		return "_"
	}
	out := strings.Builder{}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				out.WriteRune('_')
			}
			out.WriteRune(r)
		default:
			out.WriteRune('_')
		}
	}
	id := out.String()
	if id == "" {
		return "_"
	}
	return id
}
