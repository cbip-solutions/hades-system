// SPDX-License-Identifier: MIT
// verify-drift — Q2 C drift CI lint surface.
//
// Wraps a real git-backed safetynet.CommitSource around the safetynet.Drift
// detector and calls safetynet.RunPreCommitDrift. Used by:
//
// make verify-drift # CI gate; fails build on severity=hard
// .githooks/pre-commit # local pre-commit gate (spec §6.3)
//
// Exit codes (delegated to safetynet.RunPreCommitDrift):
//
// 0 — clean OR only soft findings
// 1 — at least one severity=hard finding OR validate-error (fail-closed)
package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
)

type gitSource struct{}

func (gitSource) Recent(_ context.Context, n int) ([]safetynet.Commit, error) {
	out, err := exec.Command(
		"git", "log",
		"-n", strconv.Itoa(max(n, 0)),
		"--format=%H%x1f%s%x1f%b%x1e",
	).Output()
	if err != nil {
		return nil, err
	}
	rec := strings.Split(strings.TrimRight(string(out), "\x1e\n"), "\x1e")
	commits := make([]safetynet.Commit, 0, len(rec))
	for _, r := range rec {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		f := strings.SplitN(r, "\x1f", 3)
		if len(f) < 3 {
			continue
		}
		commits = append(commits, safetynet.Commit{
			SHA:     strings.TrimSpace(f[0]),
			Subject: strings.TrimSpace(f[1]),
			Body:    strings.TrimSpace(f[2]),
		})
	}
	return commits, nil
}

type stderrEmit struct{}

func (stderrEmit) Emit(_ context.Context, _ safetynet.Event) error { return nil }

func main() {
	n := flag.Int("recent", 50, "inspect this many recent commits")
	flag.Parse()
	d := safetynet.NewDrift(gitSource{}, stderrEmit{})
	os.Exit(safetynet.RunPreCommitDrift(context.Background(), d, *n))
}
