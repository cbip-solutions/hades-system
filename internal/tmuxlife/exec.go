// SPDX-License-Identifier: MIT
package tmuxlife

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExecTmux is the canonical wrapper for invoking the tmux binary.
//
// inv-hades-117 enforcement (layer 2): every invocation MUST include "-S".
// The wrapper panics if "-S" is absent in args; this surfaces the
// programmer error immediately rather than silently spawning on the
// operator's default socket /tmp/tmux-<uid> and contaminating their
// regular tmux namespace.
//
// Args are forwarded verbatim to /usr/bin/env tmux <args...>. Callers
// should NOT pre-pend "tmux" — the wrapper rejects args[0] == "tmux"
// to surface that mistake at panic time.
//
// Returns combined stdout+stderr on success; on error returns the bytes
// captured up to failure plus the wrapped *exec.ExitError or context
// error.
//
// Context lifetime governs subprocess: ctx cancellation kills the tmux
// process via SIGKILL (Go's exec.CommandContext default). Tests use
// short timeouts to assert cancellation semantics; production callers
// should provide reasonable timeouts (5-30s typical for control-plane
// operations; longer for attach which is interactive).
//
// Concurrency tmux server is single-process, single-threaded internally,
// but multiple ExecTmux calls in parallel are safe because each spawns a
// fresh client subprocess that the server serializes.
func ExecTmux(ctx context.Context, args ...string) ([]byte, error) {

	hasDashS := false
	for i, a := range args {

		if i == 0 && a == "tmux" {
			panic(fmt.Sprintf(
				"tmuxlife.ExecTmux: args[0] = %q; pass subcommand args only (do not pre-pend %q). inv-hades-117 socket %s",
				a, "tmux", SocketPath,
			))
		}
		if a == "-S" {
			hasDashS = true
		}
	}
	if !hasDashS {
		panic(fmt.Sprintf(
			"tmuxlife.ExecTmux: -S flag missing in args %v; inv-hades-117 forbids default socket /tmp/tmux-<uid>; pass -S %s explicitly",
			args, SocketPath,
		))
	}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {

		if ctxErr := ctx.Err(); ctxErr != nil {
			return out, fmt.Errorf("tmuxlife: tmux %s: %w (output: %s)",
				strings.Join(args, " "), ctxErr, strings.TrimSpace(string(out)))
		}
		return out, fmt.Errorf("tmuxlife: tmux %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func LookPathTmux() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("%w: %v", ErrTmuxNotInstalled, err)
	}
	return nil
}

func VersionMin(ctx context.Context, minMajor, minMinor int) error {
	out, err := exec.CommandContext(ctx, "tmux", "-V").Output()
	if err != nil {
		return fmt.Errorf("tmuxlife.VersionMin: %w", err)
	}
	major, minor, perr := parseTmuxVersion(string(out))
	if perr != nil {
		return perr
	}
	if major < minMajor || (major == minMajor && minor < minMinor) {
		return fmt.Errorf("%w: have %d.%d, want >=%d.%d",
			ErrTmuxVersionTooOld, major, minor, minMajor, minMinor)
	}
	return nil
}

func parseTmuxVersion(raw string) (major, minor int, err error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "tmux ")
	s = strings.TrimPrefix(s, "next-")

	for len(s) > 0 && (s[len(s)-1] >= 'a' && s[len(s)-1] <= 'z') {
		s = s[:len(s)-1]
	}
	parts := strings.SplitN(s, ".", 2)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("tmuxlife.VersionMin: unparsable version %q", raw)
	}
	if _, err := fmt.Sscanf(parts[0], "%d", &major); err != nil {
		return 0, 0, fmt.Errorf("tmuxlife.VersionMin: parse major %q: %w", parts[0], err)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &minor); err != nil {
		return 0, 0, fmt.Errorf("tmuxlife.VersionMin: parse minor %q: %w", parts[1], err)
	}
	return major, minor, nil
}
