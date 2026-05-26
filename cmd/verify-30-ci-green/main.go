// SPDX-License-Identifier: MIT
//
// Standalone binary composed in `make verify-30-ci-green`. Fetches last
// N commits on a given branch + their CI status, classifies each
// (success / infra / flake / real), and evaluates the 50/45/2 rolling
// window per spec §1.4 inv-zen-275.
//
// Flags
//
//	--owner       GitHub repo owner       (default cbip-solutions)
//	--repo        GitHub repo name        (default zen-swarm)
//	--branch      branch to evaluate      (default main)
//	--window      window size             (default 50)
//	--json        emit JSON output (machine-readable)
//	--quarantine  flake quarantine path   (default scripts/release-gates/flake-quarantine.txt)
//	--timeout     per-call HTTP timeout   (default 2m)
//
// Reads GITHUB_TOKEN or GH_TOKEN from env (avoid rate limits and private-repo
// 404s). Per-SHA cache at ~/.cache/hades/ci/ for repeat invocations.
//
// Reads optional flake quarantine file `scripts/release-gates/flake-
// quarantine.txt` (one entry per line; comments + blank lines OK;
// auto-expire 14d per Phase G G-3). Each non-comment line MUST be
// either a plain test-name OR a full 3-token row per spec §G.3.1
// (test-name + RFC3339 timestamp + reason-tag). The wrapper accepts
// the simpler "one regex per line" form for CLI use; the spec-compliant
// 3-token form is parsed by internal/ci.LoadFlakeQuarantine (Phase G).
//
// Exit codes:
//
//	0  gate passes
//	1  gate fails (real failures exceed threshold OR sample insufficient)
//	2  GH API error / network unavailable / config error
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/ci"
)

type runOptions struct {
	owner          string
	repo           string
	branch         string
	windowSize     int
	emitJSON       bool
	quarantinePath string
	timeout        time.Duration
	stdout         io.Writer
	stderr         io.Writer
}

func main() {
	owner := flag.String("owner", "cbip-solutions", "GitHub repo owner")
	repo := flag.String("repo", "zen-swarm", "GitHub repo name")
	branch := flag.String("branch", "main", "branch to evaluate")
	windowSize := flag.Int("window", 50, "rolling window size")
	emitJSON := flag.Bool("json", false, "emit JSON output")
	quarantinePath := flag.String("quarantine", "scripts/release-gates/flake-quarantine.txt", "flake quarantine entry list (auto-expire 14d per Phase G)")
	timeout := flag.Duration("timeout", 2*time.Minute, "per-call HTTP timeout")
	flag.Parse()

	opts := runOptions{
		owner:          *owner,
		repo:           *repo,
		branch:         *branch,
		windowSize:     *windowSize,
		emitJSON:       *emitJSON,
		quarantinePath: *quarantinePath,
		timeout:        *timeout,
		stdout:         os.Stdout,
		stderr:         os.Stderr,
	}
	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintf(opts.stderr, "verify-30-ci-green: %v\n", err)

		if strings.HasPrefix(err.Error(), "FAIL:") {
			os.Exit(1)
		}
		os.Exit(2)
	}
}

func run(parent context.Context, opts runOptions) error {
	if opts.stdout == nil {
		opts.stdout = os.Stdout
	}
	if opts.stderr == nil {
		opts.stderr = os.Stderr
	}
	timeout := opts.timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	commits, err := ci.FetchLastN(ctx, opts.owner, opts.repo, opts.branch, opts.windowSize)
	if err != nil {
		return fmt.Errorf("fetch commits: %w", err)
	}
	quarantine, err := loadQuarantine(opts.quarantinePath)
	if err != nil {
		// Non-fatal: missing or unreadable quarantine file means zero
		// flakes; doctrine "fail open" for governance file but log a
		// warning so the operator notices.
		fmt.Fprintf(opts.stderr, "warn: quarantine file %s: %v (treating as empty)\n", opts.quarantinePath, err)
	}
	classified := make([]ci.CommitStatus, len(commits))
	for i, c := range commits {
		classified[i] = ci.Classify(c, quarantine)
	}
	window := ci.DefaultRollingWindow()
	window.WindowSize = opts.windowSize
	pass, reason := window.Evaluate(classified)
	if opts.emitJSON {
		report := map[string]any{
			"window":  window,
			"pass":    pass,
			"reason":  reason,
			"commits": classified,
		}
		enc := json.NewEncoder(opts.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("encode JSON report: %w", err)
		}
		if !pass {
			return fmt.Errorf("FAIL: %s", reason)
		}
		return nil
	}
	fmt.Fprintf(opts.stdout, "Rolling window: %d commits on %s (last %d analyzed)\n", opts.windowSize, opts.branch, len(classified))
	fmt.Fprintf(opts.stdout, "Thresholds: success ≥ %d / window %d / max real %d\n", window.MinSuccess, window.WindowSize, window.MaxRealFails)
	if pass {
		fmt.Fprintf(opts.stdout, "PASS: 30-CI-green gate passes (inv-zen-275)\n")
		return nil
	}
	return fmt.Errorf("FAIL: %s", reason)
}

func loadQuarantine(path string) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if i := strings.IndexAny(line, " \t"); i >= 0 {
			line = line[:i]
		}
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines, scanner.Err()
}

func ciReset() {
	ci.ResetHTTPClient()
}
