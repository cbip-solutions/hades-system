// SPDX-License-Identifier: MIT
// Package main implements the `hades` CLI wrapper binary.
//
// `hades` is the HADES system entry point spec. It is a
// thin Go wrapper that:
//
// - Sets HERMES_SKIN=hades env when invoked with no positional args,
// then exec's `hermes` (Hermes Agent) with whatever flags the
// operator passed.
// - Translates `hades dashboard|tui|panels [--panel=NAME]` to
// `zen tui [--panel=NAME]` (legacy CLI subcommand under the hood).
// - Translates `hades <other-subcommand> <args>` to
// `zen <other-subcommand> <args>` (passthrough).
// - Handles `--version` / `--help` / `--no-wizard` locally.
//
// Architecture constraints:
// - Stdlib ONLY: os, os/exec, flag, fmt. NO Cobra/spf13. NO
// imports from internal/. Rationale: keep wrapper minimal and
// isolated from daemon-side type churn.
// - invariant single-egress: wrapper NEVER bypasses `zen` for
// daemon access; only os/exec's the existing entry points.
//
// See docs/operations/hades-entry-point.md for operator-
// facing reference.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const version = "v0.17.2"

const hermesVersion = "v0.13.0+"

const defaultDaemonUDS = "/tmp/zen-swarm.sock"

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--version":
			printVersion(os.Stdout)
			os.Exit(0)
		case "--help", "-h":
			printHelp(os.Stdout)
			os.Exit(0)
		case "dashboard", "tui", "panels":

			zenArgs := append([]string{"tui"}, args[1:]...)
			os.Exit(execZen(zenArgs, nil))
		case "--no-wizard":

			remainingArgs := args[1:]

			ensureDaemonPreflight(os.Stderr)
			os.Exit(execHermes(remainingArgs, []string{
				"HERMES_SKIN=hades",
				"HADES_NO_WIZARD=1",
			}))
		default:

			os.Exit(execZen(args, nil))
		}
	}

	ensureDaemonPreflight(os.Stderr)
	os.Exit(execHermes(nil, []string{"HERMES_SKIN=hades"}))
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "HADES system %s\non the Hermes substrate (requires Hermes Agent %s)\n", version, hermesVersion)
}

// printHelp writes the HADES-branded usage block to the supplied
// io.Writer. Hand-curated for the UX bar (Hermes / Claude Code
// parity per spec §1).
//
// Caller passes os.Stdout from main(); tests pass *bytes.Buffer for
// in-process assertions. See printVersion docstring for the A-10
// coverage-lift rationale.
//
// Subcommand modes documented here MUST match the dispatch logic in
// main(); any new subcommand added in or MUST also
// be reflected here. The wrapper's --help is the single source of truth
// for HADES-recognised subcommands (separate from `zen --help` which
// lists zen's full surface and which the wrapper does NOT modify).
//
// Line-width discipline: every line below is ≤80 columns (operator
// terminal compat). When extending, verify with:
//
// bin/hades --help | awk '{print length}' | sort -nr | head -1
//
// TestPrintHelp_InProcess in main_test.go enforces the ≤80-col rule
// programmatically (so a future extension that overflows fails CI).
func printHelp(w io.Writer) {
	fmt.Fprintln(w, "HADES system — agent orchestrator on the Hermes substrate")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "USAGE:")
	fmt.Fprintln(w, "  hades                         launch chat session (HADES skin + Hermes)")
	fmt.Fprintln(w, "  hades dashboard [--panel=N]   open TUI dashboard (alias: tui, panels)")
	fmt.Fprintln(w, "  hades <subcommand> [args]     forward to legacy zen CLI (doctor, etc.)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "FLAGS:")
	fmt.Fprintln(w, "  --version                     print HADES version and exit")
	fmt.Fprintln(w, "  --help                        print this help and exit")
	fmt.Fprintln(w, "  --no-wizard                   suppress first-run onboarding wizard")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "PANELS (for hades dashboard --panel=NAME):")
	fmt.Fprintln(w, "  workforce, cost, audit, hra, confirmations, memory,")
	fmt.Fprintln(w, "  skills, doctrine, codegraph, inbox, crossproject, help")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "ENV VARS SET BY HADES:")
	fmt.Fprintln(w, "  HERMES_SKIN=hades             activates HADES banner skin in Hermes")
	fmt.Fprintln(w, "  HADES_NO_WIZARD=1             set when --no-wizard passed (Plan 18c)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "See `hermes --help` and `zen --help` for the full surfaces.")
	fmt.Fprintln(w, "Docs: docs/operations/hades-entry-point.md")
}

func execHermes(args, extraEnv []string) int {
	cmd := exec.Command("hermes", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), extraEnv...)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			maybeEmitDaemonHint(os.Stderr)
			return exitErr.ExitCode()
		}

		fmt.Fprintf(os.Stderr, "hades: cannot launch hermes: %v\n", err)
		maybeEmitDaemonHint(os.Stderr)
		return 127
	}
	return 0
}

func execZen(args, extraEnv []string) int {
	cmd := exec.Command("zen", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), extraEnv...)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			maybeEmitDaemonHint(os.Stderr)
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "hades: cannot launch zen: %v\n", err)
		maybeEmitDaemonHint(os.Stderr)
		return 127
	}
	return 0
}

// launchAgentLabel is the canonical, DEPLOYED launchd label for the
// zen-swarm-ctld daemon — NO hyphen — matching scripts/install-launchd.sh,
// configs/launchd.plist.tmpl, and `zen daemon uninstall`. A hyphenated
// variant (com.zen-swarm.<name>) is a phantom that launchctl never
// registers; it was the source of the v0.17.2 wrong-label recovery hint
// . NOTE: the sibling
// docs-cron agent genuinely uses a hyphenated label — a pre-existing
// convention drift deferred to the boundary migration; do NOT
// "fix" it here.
const launchAgentLabel = "com.zenswarm.ctld"

const preflightTimeout = 5 * time.Second

func daemonUDSPath() string {
	if p := os.Getenv("ZEN_DAEMON_UDS"); p != "" {
		return p
	}
	return defaultDaemonUDS
}

func daemonUDSPresent(udsPath string) bool {
	_, err := os.Stat(udsPath)
	return err == nil
}

func launchAgentPlistPath() (path string, installed bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	path = filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
	_, statErr := os.Stat(path)
	return path, statErr == nil
}

func kickstartDaemon() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchAgentLabel)
	return exec.Command("launchctl", "kickstart", "-k", target).Run()
}

func waitForUDS(udsPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if daemonUDSPresent(udsPath) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ensureDaemonPreflight runs on the bare `hades` → hermes path BEFORE
// exec'ing hermes (the daily entry that most needs to "just work"). Per
// ADR-0099 Option 1:
//
// - UDS already present → no-op (daemon up).
// - UDS absent + LaunchAgent installed → kickstart + wait up to 5s; if the
// daemon comes up, proceed silently (the operator never sees the blip).
// If not, emit the hint and proceed anyway.
// - UDS absent + no LaunchAgent → emit the curated install/start hint and
// proceed. We do NOT silently spawn a non-persistent daemon — that would
// hide the missing-persistence state (see ADR-0099 Alt 1).
//
// Always returns control to the caller (never blocks the operator): a
// still-down daemon is hermes' problem to degrade around, not a hard stop.
func ensureDaemonPreflight(w io.Writer) {
	udsPath := daemonUDSPath()
	if daemonUDSPresent(udsPath) {
		return
	}
	_, installed := launchAgentPlistPath()
	if installed {
		_ = kickstartDaemon()
		if waitForUDS(udsPath, preflightTimeout) {
			return
		}
	}
	emitDaemonHint(w, udsPath, installed)
}

func emitDaemonHint(w io.Writer, udsPath string, agentInstalled bool) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "HADES: daemon not running")
	fmt.Fprintf(w, "  No daemon at %s.\n", udsPath)
	if agentInstalled {
		fmt.Fprintln(w, "  → restart it:  hades daemon start")
		fmt.Fprintf(w, "     (launchd-managed: launchctl kickstart -k gui/%d/%s)\n", os.Getuid(), launchAgentLabel)
	} else {
		fmt.Fprintln(w, "  → install (persistent, auto-start at login):  hades daemon install")
		fmt.Fprintln(w, "  → or start once:                              hades daemon start")
	}
	fmt.Fprintln(w, "  See docs/operations/hades-entry-point.md §4.2")
}

func maybeEmitDaemonHint(w io.Writer) {
	udsPath := daemonUDSPath()
	if daemonUDSPresent(udsPath) {
		return
	}
	_, installed := launchAgentPlistPath()
	emitDaemonHint(w, udsPath, installed)
}
