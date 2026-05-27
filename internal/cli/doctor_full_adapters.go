// SPDX-License-Identifier: MIT
// Package cli — doctor_full_adapters.go.
//
// Constructs the release-9 subsystem doctor check.Check instances that
// `hades doctor full` composes alongside the 4 NEW release checks per
// spec §2.5 line 229 ("Composing aggregator over Plans 1-9 per-flag
// checks (--knowledge/--scheduler/--inbox/--tmux/--merge/--hermes) plus
// 4 new release checks").
//
// Import-cycle note: internal/doctor/check/cliadapter imports internal/cli
// (for ProbeResult/ProbeStatus). If this file imported cliadapter, the
// cycle would close: cli → cliadapter → cli. To avoid it, the adapter
// type is declared LOCALLY in this file as cliProbeCheckAdapter — a
// minimal check.Check impl that wraps an inline ProbeFunc returning
// []ProbeResult and collapses to a composite DiagnosticResult by
// selecting the worst Status. Semantics match cliadapter.CLIProbeAdapter;
// the duplication is the cost of preserving invariant's package
// boundaries without restructuring the whole adapter chain.
//
// Adapter wiring contract per plan F-tail F-imp:
//
// - Each release-9 prober function (RunKnowledgeProbe / RunSchedulerProbe
// / RunInboxProbe / RunTmuxProbe / RunMergeChecks / RunHermesChecks /
// RunBypassChecks / etc) is wrapped via the local adapter constructor.
// - The adapter func builds a fresh client per invocation (lazy daemon
// dial) so doctor full never panics on daemon-down — the per-probe
// defensive paths surface daemon-down as one ProbeFail row each
// (operator-actionable; first remediation: `hades daemon start`).
// - Categories are assigned per spec §3.3 enum: Configuration for
// subsystem-config probes; Connectivity for daemon-reachability;
// Hints for info-only.
//
// Test seam: BuildPlan1To9DoctorFullAdaptersForTesting overrides the
// production constructor — used by doctor full integration tests that
// want to bypass the daemon dial.
package cli

import (
	"context"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type cliProbeCheckAdapter struct {
	name        string
	category    check.Category
	description string
	probeFunc   func(ctx context.Context) []ProbeResult
}

func (a *cliProbeCheckAdapter) Name() string { return a.name }

func (a *cliProbeCheckAdapter) Category() check.Category { return a.category }

func (a *cliProbeCheckAdapter) Description() string { return a.description }

func (a *cliProbeCheckAdapter) IsDestructive() bool { return false }

func (a *cliProbeCheckAdapter) Fix(_ context.Context, _ check.FixMode) error {
	return nil
}

func (a *cliProbeCheckAdapter) Run(ctx context.Context) check.DiagnosticResult {
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
		s := translateProbeStatusToCheckStatus(r.Status)
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
		d.Detail = strings.Join(detailLines, "\n")
	}
	return d
}

// translateProbeStatusToCheckStatus maps cli.ProbeStatus iota into
// check.Status iota. Mirrors cliadapter.translateCLIStatus to preserve
// the canonical translation contract.
//
// FORBIDDEN cast: callers MUST NOT use `check.Status(int(s))`. The first
// 3 values happen to align today (ProbeOK=0/StatusPass=0 etc) but the
// alignment is incidental and may drift. Always go through this
// translation.
func translateProbeStatusToCheckStatus(s ProbeStatus) check.Status {
	switch s {
	case ProbeOK:
		return check.StatusPass
	case ProbeWarn:
		return check.StatusWarn
	case ProbeFail:
		return check.StatusFail
	default:
		return check.StatusSkip
	}
}

func newCLIProbeAdapter(name string, category check.Category, description string, fn func(ctx context.Context) []ProbeResult) check.Check {
	return &cliProbeCheckAdapter{
		name:        name,
		category:    category,
		description: description,
		probeFunc:   fn,
	}
}

func BuildPlan1To9DoctorFullAdapters(udsPath string) []check.Check {
	if buildPlan1To9DoctorFullAdaptersOverride != nil {
		return buildPlan1To9DoctorFullAdaptersOverride()
	}

	clientFactory := func() *client.Client {
		return client.New(udsPath)
	}

	out := []check.Check{}

	out = append(out, newCLIProbeAdapter(
		"subsystem.knowledge",
		check.CategoryConfiguration,
		"Plan 7 knowledge subsystem (FTS5 index, indexer, watcher) — 5 aspects",
		func(ctx context.Context) []ProbeResult {
			deps := DoctorDeps{Client: clientFactory()}
			rs, _ := RunKnowledgeProbeWithDeps(ctx, deps)
			return rs
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.scheduler",
		check.CategoryConfiguration,
		"Plan 7 scheduler subsystem (queue, missed fires, WFQ) — 4 aspects",
		func(ctx context.Context) []ProbeResult {
			deps := DoctorDeps{Client: clientFactory()}
			rs, _ := RunSchedulerProbeWithDeps(ctx, deps)
			return rs
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.inbox",
		check.CategoryConfiguration,
		"Plan 7 inbox subsystem (aggregator cache, outbox, dedup) — 4 aspects",
		func(ctx context.Context) []ProbeResult {
			deps := DoctorDeps{Client: clientFactory()}
			rs, _ := RunInboxProbeWithDeps(ctx, deps)
			return rs
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.tmux",
		check.CategoryConfiguration,
		"Plan 7 tmux subsystem (binary, server, sessions, drift) — 5 aspects",
		func(ctx context.Context) []ProbeResult {
			deps := DoctorDeps{Client: clientFactory()}
			rs, _ := RunTmuxProbeWithDeps(ctx, deps)
			return rs
		},
	))

	out = append(out, newCLIProbeAdapter(
		"daemon.reachable",
		check.CategoryRuntime,
		"Daemon UDS liveness + uptime (Plan 1+ surface)",
		func(ctx context.Context) []ProbeResult {
			deps := DoctorDeps{Client: clientFactory()}
			return runDaemonReachable(ctx, deps)
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.bypass",
		check.CategoryConfiguration,
		"Plan 2 bypass subsystem (config + transport + tokens) — 10 checks",
		func(ctx context.Context) []ProbeResult {
			c := clientFactory()
			results := runBypassChecks(ctx, c)
			return checkResultsToProbeResults(results)
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.merge",
		check.CategoryConfiguration,
		"Plan 6 merge subsystem (daemon, git, eventlog, cache) — 4 checks",
		func(ctx context.Context) []ProbeResult {
			c := clientFactory()
			mergeClient := client.NewMergeClient(c.HTTPClient(), c.BaseURL())
			results := runMergeChecks(ctx, mergeClient)
			return checkResultsToProbeResults(results)
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.hermes",
		check.CategoryConfiguration,
		"Plan 11 hermes integration (plugin format, events) — 4 checks",
		func(ctx context.Context) []ProbeResult {
			c := clientFactory()
			results := runHermesChecks(ctx, c)
			return checkResultsToProbeResults(results)
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.augment",
		check.CategoryConfiguration,
		"Plan 11 augmentation substrate (5-lane RRF) — 6 checks",
		func(ctx context.Context) []ProbeResult {
			c := clientFactory()
			results := runAugmentChecks(ctx, c)
			return checkResultsToProbeResults(results)
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.citation",
		check.CategoryConfiguration,
		"Plan 11 citation system (provenance + reproducibility) — 3 checks",
		func(ctx context.Context) []ProbeResult {
			c := clientFactory()
			results := runCitationChecks(ctx, c)
			return checkResultsToProbeResults(results)
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.coordination",
		check.CategoryConfiguration,
		"Plan 11 coordination (cross-subsystem messaging) — 2 checks",
		func(ctx context.Context) []ProbeResult {
			c := clientFactory()
			results := runCoordinationChecks(ctx, c)
			return checkResultsToProbeResults(results)
		},
	))

	out = append(out, newCLIProbeAdapter(
		"subsystem.orchestrator",
		check.CategoryConfiguration,
		"Plan 3 orchestrator engine (dispatcher, workers, recovery)",
		func(ctx context.Context) []ProbeResult {
			c := clientFactory()
			results := runOrchestratorChecks(ctx, c)
			return checkResultsToProbeResults(results)
		},
	))

	return out
}

func checkResultsToProbeResults(results []CheckResult) []ProbeResult {
	out := make([]ProbeResult, 0, len(results))
	for _, r := range results {
		out = append(out, ProbeResult{
			Name:    r.Name,
			Status:  checkResultStatusToProbeStatus(r.Status),
			Message: r.Detail,
			Detail:  r.Detail,
			Hint:    r.Hint,
		})
	}
	return out
}

func checkResultStatusToProbeStatus(s string) ProbeStatus {
	switch s {
	case "ok":
		return ProbeOK
	case "warn":
		return ProbeWarn
	case "fail":
		return ProbeFail
	default:
		return ProbeFail
	}
}

var buildPlan1To9DoctorFullAdaptersOverride func() []check.Check

// BuildPlan1To9DoctorFullAdaptersForTesting installs a test-only override
// for the release-9 adapter list construction. Returns a cleanup function
// callers MUST defer.
func BuildPlan1To9DoctorFullAdaptersForTesting(adapters func() []check.Check) func() {
	prev := buildPlan1To9DoctorFullAdaptersOverride
	buildPlan1To9DoctorFullAdaptersOverride = adapters
	return func() { buildPlan1To9DoctorFullAdaptersOverride = prev }
}
