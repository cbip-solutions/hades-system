// SPDX-License-Identifier: MIT
// Package aggregator — output_human.go ships the streaming-style
// human-readable renderer with `--spotlight` (hide pass rows) +
// `--ascii` (non-emoji fallback) flags per Q5=C+ orthogonal-flag set.
//
// ships the post-Run batched variant (renders the fully-populated
// Report); wires the channel-based streaming variant when the
// `--format=human-stream` flag lands.
package aggregator

import (
	"fmt"
	"io"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type HumanOptions struct {
	Spotlight bool
	ASCII     bool
	NoColor   bool
}

func RenderHumanStream(w io.Writer, report *Report, opts HumanOptions) {
	if report == nil {
		_, _ = fmt.Fprintln(w, "no diagnostics")
		return
	}
	for _, d := range report.Diagnostics {
		if opts.Spotlight && d.Status == check.StatusPass {
			continue
		}
		glyph := d.Status.Glyph(opts.ASCII)
		_, _ = fmt.Fprintf(w, "  %s %-40s %s\n", glyph, truncateName(d.Name), d.Message)
		if d.Detail != "" {
			for _, line := range strings.Split(d.Detail, "\n") {
				_, _ = fmt.Fprintf(w, "      %s\n", line)
			}
		}
		if d.Hint != "" {
			_, _ = fmt.Fprintf(w, "      hint: %s\n", d.Hint)
		}
	}
	_, _ = fmt.Fprintf(w, "\nSummary: %d pass, %d warn, %d fail, %d skip\n",
		report.PassCount, report.WarnCount, report.FailCount, report.SkipCount)
	if report.AuditEventHash != "" {
		_, _ = fmt.Fprintf(w, "Audit event: %s\n", report.AuditEventHash)
	}
}

func truncateName(name string) string {
	const maxNameLen = 40
	if len(name) <= maxNameLen {
		return name
	}
	return name[:maxNameLen-3] + "..."
}
