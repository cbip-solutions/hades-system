// SPDX-License-Identifier: MIT
// Package migrate (internal/doctor/migrate) ships the
// `claude-code-install-detected` doctor check (info-level hint). This is
// the HADES design surface-migration auto-detection per SOTA-2 "surface
// migration when state detected" pattern (design choice rationale).
//
// Boundary (invariant): migrate doctor sub-package consumes ONLY
// internal/doctor/check; MUST NOT import internal/store. The check is
// read-only (no recursion into local agent memory/; only top-level marker probe).
//
// Adversarial-safety note (spec §6.1): the top-level scan avoids the
// hostile-input recursion attack surface where an attacker plants a
// fake "settings.json" deep in local agent memory/ to trigger a false migration
// hint. Only top-level marker files / directories are probed.
package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type DetectCheck struct {
	homeDir string
}

type DetectCheckConfig struct {
	HomeDir string
}

func NewDetectCheck(cfg DetectCheckConfig) *DetectCheck {
	home := cfg.HomeDir
	if home == "" {

		home, _ = os.UserHomeDir()
	}
	return &DetectCheck{homeDir: home}
}

func (c *DetectCheck) Name() string { return "claude-code.install-detected" }

func (c *DetectCheck) Category() check.Category { return check.CategoryHints }

func (c *DetectCheck) Description() string {
	return "Local agent install detected at ~/local agent config/ (auto-surface migrate hint)"
}

func (c *DetectCheck) IsDestructive() bool { return false }

func (c *DetectCheck) Fix(_ context.Context, _ check.FixMode) error { return nil }

var topLevelMarkers = []string{
	"settings.json",
	"skills",
	"commands",
}

func (c *DetectCheck) Run(_ context.Context) check.DiagnosticResult {
	d := check.DiagnosticResult{Name: c.Name(), Status: check.StatusPass}
	if c.homeDir == "" {
		d.Status = check.StatusSkip
		d.Message = "no $HOME resolvable; skipping"
		return d
	}
	claudeDir := filepath.Join(c.homeDir, "local agent config")
	info, err := os.Stat(claudeDir)
	if err != nil || !info.IsDir() {
		d.Message = "no ~/local agent config/ detected"
		return d
	}

	found := []string{}
	for _, m := range topLevelMarkers {
		mp := filepath.Join(claudeDir, m)
		if _, err := os.Stat(mp); err == nil {
			found = append(found, m)
		}
	}
	if len(found) == 0 {
		d.Message = "~/local agent config/ exists but no migration markers detected"
		return d
	}
	d.Message = fmt.Sprintf("local agent memory/ detected with %d marker(s); migration available", len(found))
	d.Hint = "run `hades migrate claude-code --dry-run` to preview migration plan"
	return d
}
