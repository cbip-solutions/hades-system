// SPDX-License-Identifier: MIT
package safetynet

import (
	"context"
	"fmt"
	"io"
	"os"
)

func RunPreCommitDrift(ctx context.Context, d *Drift, n int) int {
	return runPreCommitDriftTo(ctx, d, n, os.Stderr)
}

func runPreCommitDriftTo(ctx context.Context, d *Drift, n int, w io.Writer) int {
	rep, err := d.Validate(ctx, n)
	if err != nil {
		fmt.Fprintf(w, "drift hook: validate failed: %v\n", err)
		return 1
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(w, "[drift %-4s] %s  rule=%s  %s\n",
			string(f.Severity), f.CommitSHA, f.Rule, f.Detail)
	}
	if rep.MaxSeverity == SeverityHard {
		fmt.Fprintln(w, "drift hook: BLOCKED (invariant — substrate drift severity=hard)")
		return 1
	}
	return 0
}
