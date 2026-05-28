// SPDX-License-Identifier: MIT
// Package mcp (internal/doctor/mcp) ships the `reviewed-MCP-availability`
// doctor check (invariant MCP risk tiers + reviewed catalog availability).
//
// Per design choice 4-tier reviewed MCP set, the check probes per-MCP availability
// via the package-manager-specific seam (npm / pip / binary).
// catalog (`internal/onboard/mcp/catalog.go`) is the source-of-truth for
// the reviewed entries; this check is a consumer (a translation adapter
// from internal/onboard/mcp.Entry to MCPSpec lives in production wiring,
// shipped by CLI surface).
//
// Boundary (invariant): mcp doctor consumes ONLY internal/doctor/check;
// MUST NOT import internal/store. The catalog dependency is
// avoided here — the doctor's MCPSpec is a self-contained projection of
// the catalog Entry fields needed for the availability probe.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
	"golang.org/x/sync/errgroup"
)

type MCPSpec struct {
	Name           string
	Tier           int
	PackageManager string
	PackageName    string
	MinVersion     string
	RiskTier       string
}

type Prober interface {
	ProbePackage(ctx context.Context, manager, pkg string) (version string, err error)
}

var (
	ErrPackageNotFound     = errors.New("mcp: package not found in registry")
	ErrManagerNotInstalled = errors.New("mcp: package manager not installed locally")
)

type AvailabilityCheck struct {
	specs      []MCPSpec
	prober     Prober
	fixApplier fix.Applier
	emitter    fix.Emitter
}

type AvailabilityCheckConfig struct {
	Specs      []MCPSpec
	Prober     Prober
	FixApplier fix.Applier
	Emitter    fix.Emitter
}

func NewAvailabilityCheck(cfg AvailabilityCheckConfig) *AvailabilityCheck {
	return &AvailabilityCheck{
		specs:      cfg.Specs,
		prober:     cfg.Prober,
		fixApplier: cfg.FixApplier,
		emitter:    cfg.Emitter,
	}
}

func (c *AvailabilityCheck) Name() string { return "mcp.reviewed-availability" }

func (c *AvailabilityCheck) Category() check.Category { return check.CategoryConfiguration }

func (c *AvailabilityCheck) Description() string {
	return "Reviewed MCP catalog per-MCP availability + version pin (invariant)"
}

func (c *AvailabilityCheck) IsDestructive() bool { return false }

func (c *AvailabilityCheck) Fix(ctx context.Context, mode check.FixMode) error {
	if c.fixApplier == nil {
		return nil
	}
	return fix.Apply(ctx, c.fixApplier, mode, c.emitter)
}

type probeResult struct {
	Spec    MCPSpec
	Version string
	Err     error
}

// Run probes each catalog entry concurrently; the Aggregator-facing
// result collapses per-MCP status to the worst (any FAIL → FAIL; else
// any SKIP → SKIP; else any WARN → WARN; else PASS). Per-MCP detail
// is surfaced via the Detail field.
//
// Concurrency probes are independent (different package managers + pkgs)
// → run via errgroup.WithContext(ctx). The aggregator's per-check timeout
// wraps the whole Run() call; individual probes inherit cancellation via
// the errgroup's derived gctx, and the post-Wait ctx.Err() check returns
// StatusSkip if the parent ctx was cancelled before all probes finished.
//
// Ctx-honouring contract (check.go:47-48 godoc): Run MUST surface
// StatusSkip on cancellation; per-probe goroutines pass gctx through to
// the Prober seam so a well-behaved Prober propagates the cancellation
// to its own I/O (e.g., exec.CommandContext, http.Request.WithContext).
// Even if a Prober ignores ctx (defense-in-depth), the parent ctx.Err()
// check below short-circuits the result collapse.
func (c *AvailabilityCheck) Run(ctx context.Context) check.DiagnosticResult {
	d := check.DiagnosticResult{Name: c.Name()}
	if len(c.specs) == 0 {
		d.Status = check.StatusSkip
		d.Message = "MCP catalog empty (stage catalog not initialised)"
		return d
	}
	if c.prober == nil {
		d.Status = check.StatusSkip
		d.Message = "MCP prober unavailable"
		return d
	}

	if err := ctx.Err(); err != nil {
		d.Status = check.StatusSkip
		d.Message = "context cancelled before probe"
		d.Hint = "rerun `hades doctor full` without cancellation"
		return d
	}
	results := make([]probeResult, len(c.specs))
	g, gctx := errgroup.WithContext(ctx)

	for i, spec := range c.specs {
		i, spec := i, spec
		g.Go(func() error {
			version, err := c.prober.ProbePackage(gctx, spec.PackageManager, spec.PackageName)
			results[i] = probeResult{Spec: spec, Version: version, Err: err}

			return nil
		})
	}

	_ = g.Wait()

	if err := ctx.Err(); err != nil {
		d.Status = check.StatusSkip
		d.Message = "context cancelled during probe"
		d.Hint = "rerun `hades doctor full` without cancellation"
		return d
	}

	worst := check.StatusPass
	var detailLines []string
	hasIssue := false
	for _, r := range results {
		line, status := classifyResult(r)
		detailLines = append(detailLines, line)
		if severityRank(status) > severityRank(worst) {
			worst = status
		}
		if status != check.StatusPass {
			hasIssue = true
		}
	}
	d.Status = worst
	d.Message = fmt.Sprintf("%d reviewed MCPs probed", len(c.specs))
	d.Detail = strings.Join(detailLines, "\n")
	if hasIssue {
		d.Hint = "review per-MCP detail; install via `hades mcp add <name>` or run `hades doctor full --fix` for guided remediation"
	}
	return d
}

func severityRank(s check.Status) int {
	switch s {
	case check.StatusPass:
		return 0
	case check.StatusWarn:
		return 1
	case check.StatusSkip:
		return 2
	case check.StatusFail:
		return 3
	default:
		return 0
	}
}

func classifyResult(r probeResult) (line string, status check.Status) {
	prefix := fmt.Sprintf("  - %s (tier %d, %s): ", r.Spec.Name, r.Spec.Tier, r.Spec.RiskTier)
	switch {
	case errors.Is(r.Err, ErrPackageNotFound):
		return prefix + "NOT FOUND", check.StatusFail
	case errors.Is(r.Err, ErrManagerNotInstalled):
		return prefix + fmt.Sprintf("manager %s not installed; SKIP", r.Spec.PackageManager), check.StatusSkip
	case r.Err != nil:
		return prefix + fmt.Sprintf("error: %v", r.Err), check.StatusFail
	case r.Spec.MinVersion != "" && check.CompareVersions(check.ParseSemverLax(r.Version), check.ParseSemverLax(r.Spec.MinVersion)) < 0:
		return prefix + fmt.Sprintf("version %s < pin %s (WARN)", r.Version, r.Spec.MinVersion), check.StatusWarn
	default:
		return prefix + fmt.Sprintf("OK (version %s)", r.Version), check.StatusPass
	}
}
