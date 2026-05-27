// SPDX-License-Identifier: MIT
// Package doctorfull ships the `zen doctor full` Cobra subcommand
// .
//
// Lives in its own sub-package to avoid an import cycle: internal/cli →
// internal/doctor/aggregator → internal/doctor/check → internal/cli
// (the adapter shims import the cli ProbeResult/
// ProbeStatus types). By isolating the wiring code here, the cycle is
// broken — doctorfull imports aggregator + check, but cli imports
// doctorfull (one-way).
//
// Consumes
// - internal/doctor/aggregator (F1) — parallel orchestrator + Tessera
// audit emit + JSON schemaVersion=1.0 + bitmask exit codes
// - internal/doctor/check (F1) — Check interface + Status/FixMode
// - internal/doctor/fix (F3) — per-check Fix(ctx, mode) impls +
// destructive-confirm gate
// - internal/doctor/hermes (F2) — InstallCheck + PluginFormatCheck
// - internal/doctor/migrate (F2) — DetectCheck (claude-code presence)
// - internal/doctor/mcp (F2) — AvailabilityCheck (curated catalog)
//
// Q5=C+ flag set: --fix, --auto-safe, --yes, --non-interactive, --quick,
// --spotlight, --ascii, --format, --check-timeout, --no-color,
// --strict-skip.
//
// Bitmask exit codes per spec §5.2 (per aggregator.ExitCode):
//
// 0 = all pass (incl. skip when --strict-skip is false)
// 1 = any warn (OR'd)
// 2 = any fail (OR'd)
// 4 = any skip-unable-to-check (OR'd)
//
// EXIT CODES section documented in --help text (SOTA-4 anti-pattern #5
// avoidance).
//
// Catalog scope:
// The 4 NEW checks are wired by default.
// per-flag checks are adapter-wrappable via internal/doctor/check/cliadapter.NewCLIProbeAdapter
// — wiring those into this catalog is forward-additive (+
// orthogonal scope per spec §1.3); the doctor surface remains stable
// because consumers only ever iterate Report.Diagnostics.
package doctorfull

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/cache"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
	"github.com/cbip-solutions/hades-system/internal/doctor/hermes"
	"github.com/cbip-solutions/hades-system/internal/doctor/mcp"
	migratecheck "github.com/cbip-solutions/hades-system/internal/doctor/migrate"
)

type Config struct {
	RecoverableSentinel error

	Plan1To9Adapters []check.Check

	AuditEmitter aggregator.Emitter

	Plan13FixAppliers map[string]fix.Applier

	FixEmitter fix.Emitter
}

func NewDoctorFullCmd(cfg Config) *cobra.Command {
	var (
		flagFix            bool
		flagAutoSafe       bool
		flagYes            bool
		flagNonInteractive bool
		flagQuick          bool
		flagSpotlight      bool
		flagASCII          bool
		flagFormat         string
		flagCheckTimeout   time.Duration
		flagNoColor        bool
		flagStrictSkip     bool
	)
	cmd := &cobra.Command{
		Use:   "full",
		Short: "Run the full Plan 13 doctor aggregator (4 Plan-13 checks + adapter-wrapped Plan 1-9)",
		Long: `zen doctor full

Runs the Plan 13 composing aggregator over the 4 NEW Plan 13 checks
(hermes-install, plugin-format, claude-code-install-detected,
curated-MCP-availability) plus any Plan 1-9 checks registered via
internal/doctor/check/cliadapter.NewCLIProbeAdapter (forward-additive).

Severity levels (4-level per Q5=C+):

  pass / warn / fail / skip   (` + "`--ascii`" + ` swaps glyphs for legacy terminals)

Use --spotlight to hide pass rows. Use --format json for machine-
parseable output (schemaVersion=1.0). Use --quick to reuse cached
last-run output ≤5min old (` + "`--fix`" + ` invalidates cache).

EXIT CODES (bitmask, OR'd per spec §5.2):

  0 = all pass (incl. skip-only when --strict-skip is false)
  1 = any warn
  2 = any fail
  4 = any skip-unable-to-check (promoted to 2 if --strict-skip)

  Combinations: 3 = warn+fail, 5 = warn+skip, 6 = fail+skip,
                7 = all-three.

--fix interactive auto-remediation: per-check confirmation [y/N]; halts
if any fix fails. --auto-safe restricts to non-destructive ops only.
--yes skips all prompts (requires explicit operator authorization for
CI). --non-interactive errors loudly if a prompt would arise.

Cache: ` + "`~/.cache/zen-swarm/doctor/last-run.json`" + ` per-project; --quick
re-uses cached output ≤5min old; --fix invalidates cache.

Audit chain emits evt.doctor.full.run per invocation (Tessera-anchored
via Plan 9 substrate). evt.doctor.full.fix.applied per fix when --fix
is used.

See ` + "`docs/operations/doctor-full.md`" + ` for the full checks catalog +
fix semantics + backup/restore handbook.`,
		RunE: func(cmd *cobra.Command, _ []string) error {

			if flagFix && flagNonInteractive && !flagYes {
				return fmt.Errorf("doctor full: --fix --non-interactive requires --yes for unattended remediation")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()

			ttyHint := term.IsTerminal(int(os.Stdout.Fd()))
			ctx = fix.WithTTY(ctx, ttyHint)

			checks := buildDoctorFullCatalog(cfg)

			c := cache.New()
			var report *aggregator.Report
			var fromCache bool
			if flagQuick && !flagFix {
				cached, fresh, _ := c.Read(cache.DefaultTTL)
				if fresh {
					report = cached
					fromCache = true
				}
			}
			if !fromCache {
				if flagFix {
					_ = c.Invalidate()
				}
				aggCfg := aggregator.Config{
					Checks:       checks,
					CheckTimeout: flagCheckTimeout,
					Emitter:      cfg.AuditEmitter,
				}
				agg := aggregator.New(aggCfg)
				r, err := agg.Run(ctx)
				if err != nil {
					return fmt.Errorf("doctor full: aggregator: %w", err)
				}
				report = r
				_ = c.Write(report)
			}

			switch flagFormat {
			case "json":
				if err := aggregator.RenderJSON(cmd.OutOrStdout(), report); err != nil {
					return fmt.Errorf("doctor full: render JSON: %w", err)
				}
			default:
				aggregator.RenderHumanStream(cmd.OutOrStdout(), report, aggregator.HumanOptions{
					Spotlight: flagSpotlight,
					ASCII:     flagASCII,
					NoColor:   flagNoColor,
				})
			}

			if flagFix && !fromCache {
				if err := runFixLoop(ctx, checks, report, flagAutoSafe, flagYes); err != nil {
					return err
				}
			}

			code := aggregator.ExitCode(report, flagStrictSkip)
			if code != 0 {
				if cfg.RecoverableSentinel != nil {
					return fmt.Errorf("%w: doctor diagnostics surfaced (exit-code bitmask %d)",
						cfg.RecoverableSentinel, code)
				}
				return fmt.Errorf("doctor full: bitmask exit code %d", code)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagFix, "fix", false, "interactive auto-remediation (per-check [y/N] prompts)")
	cmd.Flags().BoolVar(&flagAutoSafe, "auto-safe", false, "auto-apply non-destructive fixes only")
	cmd.Flags().BoolVar(&flagYes, "yes", false, "skip all confirmations (CI use; explicit operator authorization required)")
	cmd.Flags().BoolVar(&flagNonInteractive, "non-interactive", false, "error loudly on missing required input")
	cmd.Flags().BoolVar(&flagQuick, "quick", false, "use cached last-run output if fresh (≤5min)")
	cmd.Flags().BoolVar(&flagSpotlight, "spotlight", false, "hide pass rows (focus on warn/fail/skip)")
	cmd.Flags().BoolVar(&flagASCII, "ascii", false, "use ASCII glyphs instead of Unicode (legacy terminals)")
	cmd.Flags().StringVar(&flagFormat, "format", "human", "output format: human|json")
	cmd.Flags().DurationVar(&flagCheckTimeout, "check-timeout", 5*time.Second, "per-check execution timeout")
	cmd.Flags().BoolVar(&flagNoColor, "no-color", false, "disable ANSI color escape codes")
	cmd.Flags().BoolVar(&flagStrictSkip, "strict-skip", false, "promote skip to fail-equivalent for CI gating")
	return cmd
}

func buildDoctorFullCatalog(cfg Config) []check.Check {
	if testCatalogOverride != nil {
		return testCatalogOverride()
	}

	var (
		installApplier      fix.Applier
		pluginFormatApplier fix.Applier
		mcpAvailApplier     fix.Applier
	)
	if cfg.Plan13FixAppliers != nil {
		installApplier = cfg.Plan13FixAppliers["hermes.install"]
		pluginFormatApplier = cfg.Plan13FixAppliers["hermes.plugin-format"]
		mcpAvailApplier = cfg.Plan13FixAppliers["mcp.curated-availability"]
	}
	plan13Checks := []check.Check{
		hermes.NewInstallCheck(hermes.InstallCheckConfig{
			FixApplier: installApplier,
			Emitter:    cfg.FixEmitter,
		}),
		hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
			FixApplier: pluginFormatApplier,
			Emitter:    cfg.FixEmitter,
		}),
		migratecheck.NewDetectCheck(migratecheck.DetectCheckConfig{}),
		mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
			FixApplier: mcpAvailApplier,
			Emitter:    cfg.FixEmitter,
		}),
	}

	if len(cfg.Plan1To9Adapters) > 0 {
		return append(plan13Checks, cfg.Plan1To9Adapters...)
	}
	return plan13Checks
}

var testCatalogOverride func() []check.Check

// SetDoctorFullCatalogForTesting installs a test-only override for the
// doctor catalog. Returns a cleanup function callers MUST defer.
//
// This indirection (vs a constructor arg) lets the Cobra command shape
// stay 100% identical between production and tests; the override is
// not exposed via flags or env vars.
func SetDoctorFullCatalogForTesting(catalog func() []check.Check) func() {
	prev := testCatalogOverride
	testCatalogOverride = catalog
	return func() { testCatalogOverride = prev }
}

func runFixLoop(ctx context.Context, checks []check.Check, report *aggregator.Report, autoSafe, yes bool) error {
	var mode check.FixMode
	switch {
	case yes:
		mode = check.FixModeYes
	case autoSafe:
		mode = check.FixModeAutoSafe
	default:
		mode = check.FixModeInteractive
	}
	for _, d := range report.Diagnostics {
		if d.Status == check.StatusPass {
			continue
		}

		var c check.Check
		for _, candidate := range checks {
			if candidate.Name() == d.Name {
				c = candidate
				break
			}
		}
		if c == nil {
			continue
		}
		if err := c.Fix(ctx, mode); err != nil {
			return fmt.Errorf("doctor fix %s: %w", c.Name(), err)
		}
	}
	return nil
}
