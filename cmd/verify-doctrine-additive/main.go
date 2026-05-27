// SPDX-License-Identifier: MIT
// Makefile target verify-doctrine-schema-additive-only. Exits 0 on
// compliance, 1 on violation, 2 on infra error (git failure etc).
//
// Inputs (environment):
//
// - REPO_DIR: working directory of the git repo. Default ".".
// - BASE / HEAD_REF: explicit two-point range; default "HEAD~1" /
// "HEAD". Linear-history assumption — `HEAD~1..HEAD` does not
// reflect "the changes this PR introduces" on a branch that has
// merge commits in its history. For non-linear histories prefer
// MERGE_BASE_REF.
// - MERGE_BASE_REF: when set, the binary computes the real base via
// `git merge-base $MERGE_BASE_REF $HEAD_REF` and uses that as the
// diff base. Recommended in CI: set MERGE_BASE_REF=origin/main
// and HEAD_REF=HEAD so the validator inspects "everything this
// branch changed since it diverged from main", which works
// correctly through merge commits.
//
// Either BASE or MERGE_BASE_REF may be specified; MERGE_BASE_REF
// wins when both are set. HEAD_REF is shared across both modes.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func main() {
	repoDir := os.Getenv("REPO_DIR")
	if repoDir == "" {
		repoDir = "."
	}
	head := os.Getenv("HEAD_REF")
	if head == "" {
		head = "HEAD"
	}
	base := os.Getenv("BASE")
	if base == "" {
		base = "HEAD~1"
	}
	if mb := os.Getenv("MERGE_BASE_REF"); mb != "" {

		mbase, err := computeMergeBase(repoDir, mb, head)
		if err != nil {
			fmt.Fprintf(os.Stderr, "verify-doctrine-additive: merge-base %s..%s: %v\n", mb, head, err)
			os.Exit(2)
		}
		base = mbase
	}
	res, err := doctrine.ValidateRange(repoDir, base, head)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify-doctrine-additive: %v\n", err)
		os.Exit(2)
	}
	if !res.OK {
		fmt.Fprintln(os.Stderr, "inv-zen-084 violation: doctrine schema is NOT additive-only.")
		for _, v := range res.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Add an ADR ref to the commit body matching:")
		fmt.Fprintln(os.Stderr, "  docs/decisions/NNNN-doctrine-schema-<topic>.md")
		os.Exit(1)
	}
	fmt.Println("inv-zen-084 OK: doctrine schema additive-only.")
}

func computeMergeBase(repoDir, base, head string) (string, error) {
	cmd := exec.Command("git", "merge-base", base, head)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
