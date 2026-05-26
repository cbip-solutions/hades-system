// SPDX-License-Identifier: MIT
// Command verify-comment-hygiene runs Phase K classifier scan + godoc lint.
//
// Composite Go binary mirroring cmd/verify-30-ci-green pattern (Plan 15 A-5).
// Composes scripts/verify_no_task_context_comments.sh + scripts/verify_godoc_clean.sh
// into a single CI release-gate target consumable by Phase G aggregator.
//
// Exit codes:
//
//	0 — both checks clean
//	1 — rot patterns found
//	2 — godoc violations found
//	3 — IO error (script invocation, scan failure)
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/cbip-solutions/hades-system/internal/commenthygiene"
)

func main() {

	var reports []commenthygiene.Report
	for _, root := range []string{"internal", "cmd", "plugin"} {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		r, err := commenthygiene.Scan(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: scan %s: %v\n", root, err)
			os.Exit(3)
		}
		reports = append(reports, r...)
	}

	if len(reports) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: %d task-context-rot comment lines found:\n", len(reports))
		for i, r := range reports {
			if i >= 20 {
				fmt.Fprintf(os.Stderr, "      ... and %d more\n", len(reports)-i)
				break
			}
			fmt.Fprintf(os.Stderr, "      %s:%d [%s] %s\n", r.File, r.Line, r.Decision, r.Comment)
		}
		os.Exit(1)
	}

	cmd := exec.Command("bash", "scripts/verify_godoc_clean.sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(2)
	}

	fmt.Println("OK: comment hygiene clean — zero rot + clean godoc surface")
}
