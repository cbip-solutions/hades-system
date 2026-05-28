// SPDX-License-Identifier: MIT
// Package aggregator — exit_codes.go ships the HADES design bitmask
// exit-code computation per design choice+ + spec §5.2.
//
// - bit 0 (1) = any warn
// - bit 1 (2) = any fail
// - bit 2 (4) = any skip-unable-to-check
//
// OR'd: 3 = warn+fail, 5 = warn+skip, 6 = fail+skip, 7 = all-three.
//
// SOTA-4 anti-pattern #5 ("exit-code-as-mask undiscoverable") avoided via
// explicit documentation in `hades doctor full --help` EXIT CODES section
// shipped by
package aggregator

import "github.com/cbip-solutions/hades-system/internal/doctor/check"

const (
	ExitWarnBit = 1

	ExitFailBit = 2

	ExitSkipBit = 4
)

func ExitCode(r *Report, strictSkip bool) int {
	if r == nil {
		return 0
	}
	code := 0
	for _, d := range r.Diagnostics {
		switch d.Status {
		case check.StatusWarn:
			code |= ExitWarnBit
		case check.StatusFail:
			code |= ExitFailBit
		case check.StatusSkip:
			if strictSkip {
				code |= ExitFailBit
			} else {
				code |= ExitSkipBit
			}
		}
	}
	return code
}
