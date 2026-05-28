// SPDX-License-Identifier: MIT
// Package cli — probe.go
//
// task introduces the canonical ProbeResult value type that
// every HADES design doctor probe returns. ProbeResult subsumes HADES design's
// CheckResult (which stays in doctor_checks.go for the bypass.* slice;
// the bridge in RunFullProbe adapts CheckResult → ProbeResult). The
// rendering format mirrors `flutter doctor` / `brew doctor` and matches
// the HADES design doctor output already in `internal/cli/doctor.go`.
//
// Why a fresh type instead of extending CheckResult: ProbeStatus is an
// int enum (compile-time exhaustiveness; Glyph/String methods on the
// enum) where CheckResult.Status is string ("ok"/"warn"/"fail"). The
// enum surfaces typos at compile time and lets compliance tests assert
// "every probe returned a known status".
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

// ProbeStatus enumerates the three doctor outcomes. Numeric ordering
// matters: callers MAY use `if a.Status > b.Status {... }` to select
// the "worst" probe in a slice. ProbeOK = 0, ProbeWarn = 1, ProbeFail = 2.
//
// DO NOT renumber: tests + future migrations to a sqlite probe-history
// table depend on this ordering.
type ProbeStatus int

const (
	ProbeOK ProbeStatus = iota

	ProbeWarn

	ProbeFail
)

func (s ProbeStatus) String() string {
	switch s {
	case ProbeOK:
		return "ok"
	case ProbeWarn:
		return "warn"
	case ProbeFail:
		return "fail"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

func (s ProbeStatus) Glyph() string {
	switch s {
	case ProbeOK:
		return "ok  "
	case ProbeWarn:
		return "warn"
	case ProbeFail:
		return "x   "
	default:
		return "?   "
	}
}

type ProbeResult struct {
	Name    string
	Status  ProbeStatus
	Message string
	Detail  string
	Hint    string
}

func RenderProbes(probes []ProbeResult) string {
	if len(probes) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, p := range probes {
		sb.WriteString("  ")
		sb.WriteString(p.Status.Glyph())
		sb.WriteString(" ")

		name := p.Name
		if len(name) > 40 {
			name = name[:37] + "..."
		}
		if p.Message != "" {
			sb.WriteString(fmt.Sprintf("%-40s", name))
			sb.WriteString("  ")
			sb.WriteString(p.Message)
		} else {

			sb.WriteString(name)
		}
		sb.WriteString("\n")
		if p.Detail != "" {
			for _, line := range strings.Split(p.Detail, "\n") {
				sb.WriteString("      ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}
		}
		if p.Hint != "" {
			sb.WriteString("      hint: ")
			sb.WriteString(p.Hint)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func ExitCode(probes []ProbeResult, strict bool) int {
	for _, p := range probes {
		if p.Status == ProbeFail {
			return 1
		}
		if strict && p.Status == ProbeWarn {
			return 1
		}
	}
	return 0
}

type DoctorDeps struct {
	Client *client.Client

	Strict bool

	Knowledge KnowledgeProber
	Scheduler SchedulerProber
	Inbox     InboxProber
	Tmux      TmuxProber

	TesseraProber    TesseraProber
	ChainProber      ChainProber
	LitestreamProber LitestreamProber
	RecoveryProber   RecoveryProber

	AggregatorProber    AggregatorProber
	ADRProber           ADRProber
	ResearchCacheProber ResearchCacheProber
	StateProber         StateProber

	EcosystemProber EcosystemProber
}

type TesseraProber interface {
	Probe(ctx context.Context) []ProbeResult
}

type ChainProber interface {
	Probe(ctx context.Context) []ProbeResult
}

type LitestreamProber interface {
	Probe(ctx context.Context) []ProbeResult
}

type RecoveryProber interface {
	Probe(ctx context.Context) []ProbeResult
}

type AggregatorProber interface {
	Probe(ctx context.Context) []ProbeResult
}

type ADRProber interface {
	Probe(ctx context.Context) []ProbeResult
}

type ResearchCacheProber interface {
	Probe(ctx context.Context) []ProbeResult
}

type StateProber interface {
	Probe(ctx context.Context) []ProbeResult
}

// EcosystemProber is the read-only doctor probe surface for the HADES design
// ecosystem RAG substrate.
//
// Probe returns ≥15 ProbeResults covering per-eco DB size, storage budget,
// CAS blobs, cron worker PID + last-run timestamps, symbol-index health, and
// verifier live-cmd health. The exact count MAY grow as matures;
// callers MUST NOT branch on a specific count. The minimum surface (per spec
// §5 doctor surface example) is:
//
// ecosystem.{go,python,typescript,rust}.db_size per-ecosystem DB size on disk
// ecosystem.budget invariant 4-state classification
// ecosystem.cas_blobs_shared HADES design F CAS dedup count + total size
// ecosystem.last_upstream_poll last 6h cron upstream-poll timestamp
// ecosystem.last_weekly_sweep last Sunday 03:00 integrity sweep
// ecosystem.cron.pid hades-docs-cron worker PID (or "not running")
// ecosystem.symbol_index.count in-memory symbol-existence set cardinality × 4 eco
// ecosystem.symbol_index.last_rebuild last weekly symbol-index rebuild timestamp
// ecosystem.verifier.{go,python,npm,cargo} live cmd reachable + non-empty response
//
// invariant boundary: implementations live in the daemon or in a CLI shim
// that consumes the daemon HTTP surface — never in internal/cli directly.
type EcosystemProber interface {
	Probe(ctx context.Context) []ProbeResult
}

var runFullProbeOrder = []string{
	"daemon",
	"tmux",
	"projects",
	"inbox",
	"knowledge",
	"scheduler",
	"bypass",
	"meta",
}

func RunFullProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := make([]ProbeResult, 0, 16)

	out = append(out, runDaemonReachable(ctx, deps)...)
	if len(out) > 0 && out[0].Status == ProbeFail {
		return out, nil
	}

	if err := ctx.Err(); err != nil {
		return out, nil
	}

	out = append(out, invokeTmuxProber(ctx, deps.Tmux)...)

	out = append(out, runProjectsProbe(ctx, deps)...)

	out = append(out, invokeInboxProber(ctx, deps.Inbox)...)

	out = append(out, invokeKnowledgeProber(ctx, deps.Knowledge)...)

	out = append(out, invokeSchedulerProber(ctx, deps.Scheduler)...)

	out = append(out, runBypassAdapted(ctx, deps)...)

	out = append(out, runMetaProbe(ctx, deps)...)

	return out, nil
}

func runDaemonReachable(ctx context.Context, deps DoctorDeps) []ProbeResult {
	if deps.Client == nil {
		return []ProbeResult{{
			Name:    "daemon.reachable",
			Status:  ProbeFail,
			Message: "client not configured",
			Hint:    "wire DoctorDeps.Client; programmer error if seen at runtime",
		}}
	}
	pctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	h, err := deps.Client.Health(pctx)
	if err != nil {
		return []ProbeResult{{
			Name:    "daemon.reachable",
			Status:  ProbeFail,
			Message: "unreachable",
			Detail:  err.Error(),
			Hint:    "run: hades daemon start",
		}}
	}
	return []ProbeResult{{
		Name:    "daemon.reachable",
		Status:  ProbeOK,
		Message: fmt.Sprintf("version=%s uptime=%ds", h.Version, h.UptimeSeconds),
	}}
}

func runProjectsProbe(ctx context.Context, deps DoctorDeps) []ProbeResult {
	if deps.Client == nil {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	projs, err := deps.Client.ProjectsListAll(pctx)
	if err != nil {
		return []ProbeResult{{
			Name:    "projects.list",
			Status:  ProbeWarn,
			Message: "list failed",
			Detail:  err.Error(),
			Hint:    "stage /v1/projects endpoint must be wired",
		}}
	}
	out := []ProbeResult{{
		Name:    "projects.count",
		Status:  ProbeOK,
		Message: fmt.Sprintf("%d active", len(projs)),
	}}
	for _, p := range projs {
		if p.IsArchived() {
			continue
		}
		out = append(out, runOneProjectDoctor(pctx, deps, p.Alias)...)
	}
	return out
}

func runOneProjectDoctor(ctx context.Context, deps DoctorDeps, alias string) []ProbeResult {
	r, err := deps.Client.ProjectDoctorReport(ctx, alias)
	if err != nil {
		return []ProbeResult{{
			Name:    fmt.Sprintf("projects.%s.doctor", alias),
			Status:  ProbeWarn,
			Message: "probe call failed",
			Detail:  err.Error(),
		}}
	}
	out := make([]ProbeResult, 0, len(r.Items))
	for _, item := range r.Items {
		st := ProbeOK
		switch item.Status {
		case "warn":
			st = ProbeWarn
		case "fail":
			st = ProbeFail
		}
		out = append(out, ProbeResult{
			Name:    fmt.Sprintf("projects.%s.%s", alias, item.Aspect),
			Status:  st,
			Message: item.Message,
			Detail:  item.Detail,
			Hint:    item.Hint,
		})
	}
	return out
}

var runKnowledgeProbeFunc = RunKnowledgeProbe

var runSchedulerProbeFunc = RunSchedulerProbe

var runInboxProbeFunc = RunInboxProbe

var runTmuxProbeFunc = RunTmuxProbe

func invokeKnowledgeProber(ctx context.Context, p KnowledgeProber) []ProbeResult {
	if p == nil {
		return []ProbeResult{{
			Name:    "knowledge.prober",
			Status:  ProbeWarn,
			Message: "prober not configured",
			Hint:    "stage wires knowledge.Prober; until then probe is a no-op",
		}}
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	results, err := runKnowledgeProbeFunc(pctx, p)
	if err != nil {
		return []ProbeResult{{
			Name:    "knowledge.probe.error",
			Status:  ProbeFail,
			Message: "probe call failed",
			Detail:  err.Error(),
		}}
	}
	return results
}

func invokeSchedulerProber(ctx context.Context, p SchedulerProber) []ProbeResult {
	if p == nil {
		return []ProbeResult{{
			Name:    "scheduler.prober",
			Status:  ProbeWarn,
			Message: "prober not configured",
			Hint:    "stage wires scheduler.Prober; until then probe is a no-op",
		}}
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	results, err := runSchedulerProbeFunc(pctx, p)
	if err != nil {
		return []ProbeResult{{
			Name:    "scheduler.probe.error",
			Status:  ProbeFail,
			Message: "probe call failed",
			Detail:  err.Error(),
		}}
	}
	return results
}

func invokeInboxProber(ctx context.Context, p InboxProber) []ProbeResult {
	if p == nil {
		return []ProbeResult{{
			Name:    "inbox.prober",
			Status:  ProbeWarn,
			Message: "prober not configured",
			Hint:    "stage wires inbox.Prober; until then probe is a no-op",
		}}
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	results, err := runInboxProbeFunc(pctx, p)
	if err != nil {
		return []ProbeResult{{
			Name:    "inbox.probe.error",
			Status:  ProbeFail,
			Message: "probe call failed",
			Detail:  err.Error(),
		}}
	}
	return results
}

func invokeTmuxProber(ctx context.Context, p TmuxProber) []ProbeResult {
	if p == nil {
		return []ProbeResult{{
			Name:    "tmux.prober",
			Status:  ProbeWarn,
			Message: "prober not configured",
			Hint:    "stage wires tmux.Prober; until then probe is a no-op",
		}}
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	results, err := runTmuxProbeFunc(pctx, p)
	if err != nil {
		return []ProbeResult{{
			Name:    "tmux.probe.error",
			Status:  ProbeFail,
			Message: "probe call failed",
			Detail:  err.Error(),
		}}
	}
	return results
}

func runBypassAdapted(ctx context.Context, deps DoctorDeps) []ProbeResult {
	if deps.Client == nil {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	checks := runBypassChecks(pctx, deps.Client)
	out := make([]ProbeResult, 0, len(checks))
	for _, c := range checks {
		var st ProbeStatus
		switch c.Status {
		case "ok":
			st = ProbeOK
		case "warn":
			st = ProbeWarn
		default:
			st = ProbeFail
		}
		out = append(out, ProbeResult{
			Name:    c.Name,
			Status:  st,
			Message: c.Detail,
			Hint:    c.Hint,
		})
	}
	return out
}

func runMetaProbe(ctx context.Context, deps DoctorDeps) []ProbeResult {
	if deps.Client == nil {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	m, err := deps.Client.MetaSnapshotGet(pctx)
	if err != nil {
		return []ProbeResult{{
			Name:    "meta.snapshot",
			Status:  ProbeWarn,
			Message: "snapshot call failed",
			Detail:  err.Error(),
		}}
	}
	out := make([]ProbeResult, 0, 2)
	st := ProbeOK
	if m.PanicsLast24h > 0 {
		st = ProbeFail
	}
	out = append(out, ProbeResult{
		Name:    "meta.panics.24h",
		Status:  st,
		Message: fmt.Sprintf("%d panics in last 24h", m.PanicsLast24h),
	})
	st = ProbeOK
	if m.CostUtilizationPct >= 80 {
		st = ProbeWarn
	}
	if m.CostUtilizationPct >= 100 {
		st = ProbeFail
	}
	out = append(out, ProbeResult{
		Name:    "meta.cost.utilization",
		Status:  st,
		Message: fmt.Sprintf("%d%% of daemon-level cap", m.CostUtilizationPct),
	})
	return out
}

func RunKnowledgeProbeWithDeps(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	return invokeKnowledgeProber(ctx, deps.Knowledge), nil
}

func RunSchedulerProbeWithDeps(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	return invokeSchedulerProber(ctx, deps.Scheduler), nil
}

func RunInboxProbeWithDeps(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	return invokeInboxProber(ctx, deps.Inbox), nil
}

func RunTmuxProbeWithDeps(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	return invokeTmuxProber(ctx, deps.Tmux), nil
}
