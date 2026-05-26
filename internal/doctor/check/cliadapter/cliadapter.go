// SPDX-License-Identifier: MIT
// Package cliadapter ships thin shims wrapping the existing per-flag
// doctor checks (`bypass.*`, `knowledge.*`, `scheduler.*`, `inbox.*`,
// `tmux.*`, `merge.*`, `audit.*`, `adr.*`, `research.*`, `state.*`,
// `hermes.*`) as Check interface impls.
//
// Upstream substrates DO NOT change behavior; adapters translate
// their existing cli.ProbeResult value type into the canonical
// DiagnosticResult shape. The upstream subsystems remain the
// source-of-truth for their check semantics (per spec §1.3).
//
// IMPORTANT each adapter assigns a Category to the wrapped check
// (per spec §3.3 Category enum). Future hot-fix changes MUST
// preserve adapter Category assignment — moving a check between
// categories breaks operator `--spotlight` flag stability.
//
// Cross-package conversion: cli.ProbeStatus and check.Status share names
// (ok/pass, warn, fail) but DIFFERENT enum domains. translateCLIStatus
// is the canonical translation surface — NEVER int-cast cli.ProbeStatus
// to check.Status directly (the int values happen to align for the
// first 3 values today, but that is incidental and may drift).
//
// Package boundary (Plan 13 Phase F-tail F5 refactor):
//
// This adapter lives in its own sub-package so that internal/doctor/check
// (the Plan 13 doctor Check interface + DiagnosticResult value type) can
// remain a leaf package with no upstream imports. The cycle that arose
// when the cli package consumed both internal/doctor/check (via the
// aggregator) AND was consumed by the adapter (for ProbeResult/ProbeStatus)
// is broken by parking the adapter here. Consumers wire CLIProbeAdapter
// via cliadapter.New(...) from the `cli` package or its doctorfull
// subpackage.
package cliadapter

import (
	"context"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type CLIProbeAdapter struct {
	name        string
	category    check.Category
	description string
	probeFunc   func(ctx context.Context) []cli.ProbeResult
}

type CLIProbeAdapterConfig struct {
	Name string

	Category check.Category

	Description string

	ProbeFunc func(ctx context.Context) []cli.ProbeResult
}

// NewCLIProbeAdapter constructs an adapter from the config. Description
// defaults if empty; Category zero value (CategoryPreflight) MAY be the
// intended value — callers MUST set it explicitly to avoid silent
// miscategorisation.
func NewCLIProbeAdapter(cfg CLIProbeAdapterConfig) *CLIProbeAdapter {
	desc := cfg.Description
	if desc == "" {
		desc = "Plan 1-9 per-flag check (adapted)"
	}
	return &CLIProbeAdapter{
		name:        cfg.Name,
		category:    cfg.Category,
		description: desc,
		probeFunc:   cfg.ProbeFunc,
	}
}

func (a *CLIProbeAdapter) Name() string { return a.name }

func (a *CLIProbeAdapter) Category() check.Category { return a.category }

func (a *CLIProbeAdapter) Description() string { return a.description }

func (a *CLIProbeAdapter) IsDestructive() bool { return false }

func (a *CLIProbeAdapter) Fix(_ context.Context, _ check.FixMode) error {
	return nil
}

func (a *CLIProbeAdapter) Run(ctx context.Context) check.DiagnosticResult {
	start := time.Now()
	d := check.DiagnosticResult{Name: a.name, Status: check.StatusPass}
	if a.probeFunc == nil {
		d.Status = check.StatusSkip
		d.Message = "probe function nil"
		d.Hint = "verify Plan 1-9 substrate prober wiring"
		d.DurationMs = time.Since(start).Milliseconds()
		return d
	}
	results := a.probeFunc(ctx)
	d.DurationMs = time.Since(start).Milliseconds()
	if len(results) == 0 {
		d.Status = check.StatusSkip
		d.Message = "no probe results emitted"
		d.Hint = "verify Plan 1-9 substrate prober is wired"
		return d
	}
	worst := check.StatusPass
	var worstMsg, worstHint string
	var detailLines []string
	for _, r := range results {
		s := translateCLIStatus(r.Status)
		if s > worst {
			worst = s
			worstMsg = r.Message
			worstHint = r.Hint
		}

		if r.Message != "" {
			detailLines = append(detailLines, r.Name+": "+r.Message)
		}
	}
	d.Status = worst
	d.Message = worstMsg
	d.Hint = worstHint
	if len(detailLines) > 1 {

		d.Detail = joinLines(detailLines)
	}
	return d
}

// translateCLIStatus maps Plan 7+ cli.ProbeStatus (OK/Warn/Fail) into
//
// FORBIDDEN cast: callers MUST NOT use `check.Status(int(cliStatus))`.
// Today the first 3 values happen to align (ProbeOK=0/StatusPass=0,
// ProbeWarn=1/StatusWarn=1, ProbeFail=2/StatusFail=2), but the alignment
// is incidental and may drift. Always go through this translation.
//
// cli.ProbeStatus has no Skip; missing-precondition contexts are reported
// as Fail in Plan 7+ — preserved here. The adapter's Run() may emit Skip
// when probeFunc returns no rows (separate from translation).
func translateCLIStatus(s cli.ProbeStatus) check.Status {
	switch s {
	case cli.ProbeOK:
		return check.StatusPass
	case cli.ProbeWarn:
		return check.StatusWarn
	case cli.ProbeFail:
		return check.StatusFail
	default:

		return check.StatusSkip
	}
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
