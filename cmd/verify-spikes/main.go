// SPDX-License-Identifier: MIT
//
// Walks docs/spikes/ registry, optionally re-executes each spike harness
// (via build-tag `spikes` factory functions), classifies severity,
// persists report markdown, and exits non-zero on CATASTROPHIC severity.
//
// # Modes
//
// --max-age 14d stale threshold (default 14d per amendment §2.4 D-4)
// --rerun force re-execution of all spikes regardless of age
// --offline only verify report files exist (no harness exec)
//
// Per amendment §2.4 D-4: CATASTROPHIC blocks release unconditionally
// . HIGH severity warns; LOW/MEDIUM/OK pass.
//
// Doctrine hard parts are where value lives — re-running spikes at
// release is the gate that earns release confidence; no defer.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cbip-solutions/hades-system/internal/spikes"
)

func main() {
	maxAge := flag.Duration("max-age", 14*24*time.Hour, "max spike report age before re-execution")
	rerun := flag.Bool("rerun", false, "force re-execute all spikes regardless of age")
	offline := flag.Bool("offline", false, "only verify report files exist (no harness exec)")
	dir := flag.String("dir", "docs/spikes", "spike registry directory")
	flag.Parse()

	if err := run(*dir, *maxAge, *rerun, *offline); err != nil {
		fmt.Fprintf(os.Stderr, "verify-spikes: %v\n", err)
		os.Exit(2)
	}
}

func run(dir string, maxAge time.Duration, rerun, offline bool) error {
	registry, err := spikes.LoadRegistry(dir)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	wantedSpikes := []string{
		"spike_01_provider_transport_abc",
		"spike_02_hermes_plugin_contract",
		"spike_03_mcp_result_envelope",
		"spike_04_voice_mcp_dispatch",
		"spike_05_ink_renderer_feasibility",
		"spike_06_telegram_inline_buttons",
		"spike_07_slack_block_kit",
		"spike_08_html_email_rendering",
	}

	for _, name := range wantedSpikes {
		s, ok := registry[name]
		if !ok {
			return fmt.Errorf("registry missing required spike %s (file docs/spikes/%s.md absent)", name, name)
		}
		age := time.Since(s.LastRun)
		switch {
		case offline:
			fmt.Printf("OK: %s (offline verify; report present at %s)\n", name, s.ReportPath)
		case rerun || age > maxAge:
			result, err := s.Execute()
			if err != nil {
				return fmt.Errorf("spike %s execute: %w", name, err)
			}
			if err := s.PersistReport(result); err != nil {
				return fmt.Errorf("spike %s persist: %w", name, err)
			}
			switch result.Severity {
			case spikes.SeverityCatastrophic:
				return fmt.Errorf("spike %s CATASTROPHIC: %s (inv-zen-274 blocks release)", name, result.Finding)
			case spikes.SeverityHigh:
				fmt.Fprintf(os.Stderr, "WARN: spike %s HIGH severity: %s\n", name, result.Finding)
			default:
				fmt.Printf("OK: %s re-executed (severity %s; finding: %s)\n", name, result.Severity, truncate(result.Finding, 80))
			}
		default:
			if s.LastSeverity == spikes.SeverityCatastrophic {
				return fmt.Errorf("spike %s last report CATASTROPHIC (age %v ≤ %v fresh): %s", name, age, maxAge, s.LastFinding)
			}
			fmt.Printf("OK: %s (age %v ≤ %v; severity %s)\n", name, age.Truncate(time.Hour), maxAge, s.LastSeverity)
		}
	}

	fmt.Printf("\nALL 8 PHASE 0 SPIKES PASS (max-age=%v; offline=%v; rerun=%v)\n", maxAge, offline, rerun)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
