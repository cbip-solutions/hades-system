// SPDX-License-Identifier: MIT
// zen-prev — thin wrapper that routes argv to an installed N-1 release binary.
// The Q2 C safety-net's "fall back to last known good" handle.
//
// Resolution order for the target binary path:
// 1. $ZEN_PREV_TARGET_PATH (test/override hook)
// 2. $XDG_DATA_HOME/zen-swarm/prev/zen (Linux/macOS standard)
// 3. ~/.local/share/zen-swarm/prev/zen (XDG fallback)
//
// Exit codes:
//
// 0 — target ran and returned 0
// 2 — target binary not installed (graceful refuse, NOT a crash)
// * — target's exit code (propagated when target ran but failed)
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func resolveTarget() (string, error) {
	if p := os.Getenv("ZEN_PREV_TARGET_PATH"); p != "" {
		return p, nil
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "zen-swarm", "prev", "zen"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home: %w", err)
	}
	return filepath.Join(home, ".local", "share", "zen-swarm", "prev", "zen"), nil
}

// run is the testable entry point: it returns the intended exit code
// instead of calling os.Exit so test harnesses can drive it directly.
//
// The stdin/stdout/stderr wiring on cmd is process-inheriting (the wrapper
// is a thin shell — it MUST NOT buffer or transform the target's IO).
func run(args []string) int {
	target, err := resolveTarget()
	if err != nil {
		fmt.Fprintf(os.Stderr, "zen-prev: resolve target: %v\n", err)
		return 2
	}
	if _, err := os.Stat(target); err != nil {
		fmt.Fprintf(os.Stderr, "zen-prev: not installed (%s). Run `zen safetynet prev install` first.\n", target)
		return 2
	}
	cmd := exec.Command(target, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "zen-prev: target invocation failed: %v\n", err)
		return 2
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:]))
}
