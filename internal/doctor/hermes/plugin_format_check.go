// SPDX-License-Identifier: MIT
// Package hermes — plugin_format_check.go ships the `hermes.plugin-format`
// doctor check (inv-zen-176 + inv-zen-190). Validates plugin directory
// shape matches Phase H' canonical Hermes plugin format; rejects CC
// remnants + OpenClaude (legacy) markers.
//
// Boundary (inv-zen-031): consumes ONLY internal/doctor/check + the
// PluginPathResolver injection seam; MUST NOT import internal/store.
package hermes

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
)

var ErrResolverFailed = errors.New("hermes: plugin path resolver failed")

type PluginPathResolver interface {
	ResolvePluginPath(ctx context.Context, projectID string) (path string, scope string, err error)
}

type PluginFormatCheck struct {
	resolver   PluginPathResolver
	fixApplier fix.Applier
	emitter    fix.Emitter
}

type PluginFormatCheckConfig struct {
	Resolver   PluginPathResolver
	FixApplier fix.Applier
	Emitter    fix.Emitter
}

func NewPluginFormatCheck(cfg PluginFormatCheckConfig) *PluginFormatCheck {
	return &PluginFormatCheck{
		resolver:   cfg.Resolver,
		fixApplier: cfg.FixApplier,
		emitter:    cfg.Emitter,
	}
}

func (c *PluginFormatCheck) Name() string { return "hermes.plugin-format" }

func (c *PluginFormatCheck) Category() check.Category { return check.CategoryPreflight }

func (c *PluginFormatCheck) Description() string {
	return "Hermes plugin format match + reject CC/OpenClaude remnants (inv-zen-176 + 190)"
}

func (c *PluginFormatCheck) IsDestructive() bool { return true }

func (c *PluginFormatCheck) Fix(ctx context.Context, mode check.FixMode) error {
	if c.fixApplier == nil {
		return nil
	}
	return fix.Apply(ctx, c.fixApplier, mode, c.emitter)
}

var canonicalFiles = []string{
	"plugin.yaml",
	"commands",
	"hooks",
	"skills",
}

// remnantMarkers signal CC (Claude-Code legacy) or OpenClaude scaffolds
// that have NOT been migrated to Hermes plugin format. Their presence is
// inv-zen-176 violation; the check returns StatusFail with hint to run
// `zen migrate claude-code` first.
//
// SECURITY this slice is the canonical taxonomy of legacy markers;
// additions require ADR amendment + matching update to
// internal/migrate/source/ scanner so detect + migrate stay in lockstep.
var remnantMarkers = []string{
	"settings.json",
	"agent.json",
	"openclaw.toml",
}

// Run probes the resolved plugin directory + validates format.
//
// Decision tree:
//  1. Context cancelled → StatusSkip (defense-in-depth per Check.Run godoc)
//  2. Resolver error → StatusSkip + scaffold hint
//  3. Directory missing → StatusFail + config-init hint
//  4. Path not directory → StatusFail
//  5. Remnant marker present → StatusFail + migrate hint (inv-zen-176)
//  6. Canonical file missing → StatusFail + scaffold hint (inv-zen-190)
//  7. All checks pass → StatusPass
//
// Context cancellation: per spec §3.3 + check.go:47-48 godoc contract,
// Run MUST honour ctx.Done() and emit StatusSkip on cancellation. The
// inner os.Stat loops perform fast filesystem stat calls (typically <1ms
// per entry) but the canonicalFiles + remnantMarkers slices can grow;
// each loop iteration probes ctx.Err() so cancellation interrupts within
// 1 syscall boundary.
func (c *PluginFormatCheck) Run(ctx context.Context) check.DiagnosticResult {
	d := check.DiagnosticResult{Name: c.Name()}
	if err := ctx.Err(); err != nil {
		d.Status = check.StatusSkip
		d.Message = "context cancelled before plugin format probe"
		d.Hint = "rerun `zen doctor full` without cancellation"
		return d
	}
	if c.resolver == nil {
		d.Status = check.StatusSkip
		d.Message = "no plugin path resolver wired"
		d.Hint = "run `zen config init` or `zen migrate claude-code` to scaffold plugin"
		return d
	}
	pluginPath, scope, err := c.resolver.ResolvePluginPath(ctx, "")
	if err != nil {
		d.Status = check.StatusSkip
		d.Message = fmt.Sprintf("plugin path resolver failed: %v", err)
		d.Hint = "run `zen config init` or `zen migrate claude-code` to scaffold plugin"
		return d
	}
	info, err := os.Stat(pluginPath)
	if errors.Is(err, fs.ErrNotExist) {
		d.Status = check.StatusFail
		d.Message = fmt.Sprintf("plugin directory missing: %s (scope=%s)", pluginPath, scope)
		d.Hint = "run `zen config init` to scaffold Hermes plugin"
		return d
	}
	if err != nil {
		d.Status = check.StatusFail
		d.Message = fmt.Sprintf("plugin directory stat failed: %v", err)
		return d
	}
	if !info.IsDir() {
		d.Status = check.StatusFail
		d.Message = fmt.Sprintf("plugin path not a directory: %s", pluginPath)
		return d
	}

	for _, marker := range remnantMarkers {
		if err := ctx.Err(); err != nil {
			d.Status = check.StatusSkip
			d.Message = "context cancelled during remnant scan"
			d.Hint = "rerun `zen doctor full` without cancellation"
			return d
		}
		markerPath := filepath.Join(pluginPath, marker)
		if _, err := os.Stat(markerPath); err == nil {
			d.Status = check.StatusFail
			d.Message = fmt.Sprintf("plugin remnant detected: %s (CC/OpenClaude legacy)", marker)
			d.Hint = "run `zen migrate claude-code --backup-target ~/.local/state/zen-swarm/migrate-backups/$(date +%Y%m%dT%H%M%S)/` then re-run doctor"
			return d
		}
	}

	missing := []string{}
	for _, fname := range canonicalFiles {
		if err := ctx.Err(); err != nil {
			d.Status = check.StatusSkip
			d.Message = "context cancelled during canonical-file scan"
			d.Hint = "rerun `zen doctor full` without cancellation"
			return d
		}
		fpath := filepath.Join(pluginPath, fname)
		if _, err := os.Stat(fpath); errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, fname)
		}
	}
	if len(missing) > 0 {
		d.Status = check.StatusFail
		d.Message = fmt.Sprintf("plugin missing canonical files at %s: %s", pluginPath, strings.Join(missing, ", "))
		d.Hint = "run `zen config init` to scaffold Hermes plugin format"
		return d
	}

	d.Status = check.StatusPass
	d.Message = fmt.Sprintf("plugin format canonical at %s (scope=%s)", pluginPath, scope)
	return d
}
