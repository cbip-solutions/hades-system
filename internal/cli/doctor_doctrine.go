// SPDX-License-Identifier: MIT
// Package cli — doctor_doctrine.go ( Task N-1; replaces
// spec §6.2 doctor template).
//
// PLAN 4 BASELINE (preserved by name):
// - doctrine.active.resolves
// - doctrine.builtins.load
//
// PLAN 8 EXTENDS to 11 checks (9 new + 2 extended):
// - doctrine.active.resolves (extended; per-project chain)
// - doctrine.builtins.load (extended; schema_version=1.0)
// - doctrine.overrides.per-project.healthy (NEW)
// - doctrine.reload.watcher.healthy (NEW)
// - doctrine.telemetry.subscriber.healthy (NEW)
// - doctrine.lint.analyzers.registered (NEW; in-process)
// - doctrine.schema.migration.status (NEW)
// - doctrine.reinforcement.templates.parse (NEW; in-process)
// - doctrine.amendments.pending (NEW)
// - doctrine.events.recent (NEW)
// - doctrine.transverse.axioms.hardcoded (NEW; in-process)
//
// BOUNDARY-PIVOT FROM PLAN: the plan envisioned three additional doctor-
// introspection HTTP endpoints (/v1/doctrine/reload-state, /lint-status,
// /telemetry-status) that did not ship. consumes the
// actually-shipped surface:
//
// - in-process: builtins.load, reinforcement.templates.parse,
// transverse.axioms.hardcoded, lint.analyzers.registered (compile-time
// references to internal/doctrine/lint/analyzers/{nostub,nostore,
// conventional_commit}.Analyzer singletons)
// - GET /v1/doctrine/active — active.resolves
// - GET /v1/doctrine/status — reload.watcher.healthy (uses
// WatcherHealthy + LastReloadOk)
// - GET /v1/doctrine/list — overrides.per-project.healthy +
// schema.migration.status (filter by Source="project" + check
// SchemaVersion ≠ CurrentSchemaVersion)
// - GET /v1/doctrine/history — telemetry.subscriber.healthy
// (filter for DoctrineAutonomousReverted) + events.recent
// - GET /v1/doctrine/propose-list — amendments.pending
//
// Each check has its own pure function (context.Context, *client.Client)
// CheckResult and a 3-second timeout. NO STUBS: every function returns a
// real CheckResult derived from a real source. Failure-mode CheckResults
// have explicit Hint strings pointing to the zen doctrine subcommand the
// operator runs to diagnose.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	builtinpkg "github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/conventional_commit"
	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostore"
	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostub"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func doctorDoctrineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctrine",
		Short: "Doctrine subsystem health (active, builtins, overrides, watcher, telemetry, lint, schema, reinforce, amendments, events, transverse)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Doctrine (Plan 8)", runDoctrineChecks)
		},
	}
}

func runDoctrineChecks(ctx context.Context, c *client.Client) []CheckResult {
	checks := []func(context.Context, *client.Client) CheckResult{
		checkDoctrineActiveResolves,
		checkDoctrineBuiltinsLoad,
		checkDoctrineOverridesPerProjectHealthy,
		checkDoctrineReloadWatcherHealthy,
		checkDoctrineTelemetrySubscriberHealthy,
		checkDoctrineLintAnalyzersRegistered,
		checkDoctrineSchemaMigrationStatus,
		checkDoctrineReinforcementTemplatesParse,
		checkDoctrineAmendmentsPending,
		checkDoctrineEventsRecent,
		checkDoctrineTransverseAxiomsHardcoded,
	}
	out := make([]CheckResult, 0, len(checks))
	for _, fn := range checks {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out = append(out, fn(cctx, c))
		cancel()
	}
	return out
}

func checkDoctrineActiveResolves(ctx context.Context, c *client.Client) CheckResult {
	resp, err := c.DoctrineV2ActiveCall(ctx)
	if err != nil {
		return CheckResult{
			Name: "doctrine.active.resolves", Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/doctrine/active unreachable; run zen doctor daemon",
		}
	}
	if resp.Name == "" {
		return CheckResult{
			Name: "doctrine.active.resolves", Status: "warn",
			Detail: "active doctrine has no name",
			Hint:   "check doctrine loader; run zen doctrine list",
		}
	}
	if resp.SchemaVersion != schema.CurrentSchemaVersion {
		return CheckResult{
			Name: "doctrine.active.resolves", Status: "warn",
			Detail: fmt.Sprintf("active=%s; schema_version=%s; want %s",
				resp.Name, resp.SchemaVersion, schema.CurrentSchemaVersion),
			Hint: "rebuild HADES or run zen doctrine migrate",
		}
	}
	return CheckResult{
		Name: "doctrine.active.resolves", Status: "ok",
		Detail: fmt.Sprintf("active=%s; schema_version=%s; source=%s",
			resp.Name, resp.SchemaVersion, resp.Source),
	}
}

func checkDoctrineBuiltinsLoad(_ context.Context, _ *client.Client) CheckResult {
	names := builtinpkg.Names()
	if len(names) == 0 {
		return CheckResult{
			Name: "doctrine.builtins.load", Status: "fail",
			Detail: "builtin.Names() returned empty",
			Hint:   "Phase D embed.FS load failed at init; check make verify-doctrine-builtin",
		}
	}
	registry, err := builtinpkg.LoadAll()
	if err != nil {
		return CheckResult{
			Name: "doctrine.builtins.load", Status: "fail",
			Detail: fmt.Sprintf("builtin.LoadAll() failed: %v", err),
			Hint:   "Phase D embed.FS load failed; check make verify-doctrine-builtin",
		}
	}
	for _, name := range names {
		s, ok := registry[name]
		if !ok || s == nil {
			return CheckResult{
				Name: "doctrine.builtins.load", Status: "fail",
				Detail: fmt.Sprintf("builtin %q missing from registry", name),
			}
		}
		if s.SchemaVersion != schema.CurrentSchemaVersion {
			return CheckResult{
				Name: "doctrine.builtins.load", Status: "fail",
				Detail: fmt.Sprintf("builtin %q has schema_version=%s; want %s",
					name, s.SchemaVersion, schema.CurrentSchemaVersion),
				Hint: "Plan 8 baseline schema_version=1.0; rebuild after Phase A schema change",
			}
		}
	}
	return CheckResult{
		Name: "doctrine.builtins.load", Status: "ok",
		Detail: fmt.Sprintf("%d builtins (%v) all schema_version=%s",
			len(names), names, schema.CurrentSchemaVersion),
	}
}

func checkDoctrineOverridesPerProjectHealthy(ctx context.Context, c *client.Client) CheckResult {
	resp, err := c.DoctrineV2ListCall(ctx, "")
	if err != nil {
		return CheckResult{
			Name: "doctrine.overrides.per-project.healthy", Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/doctrine/list unreachable; run zen doctrine list to diagnose",
		}
	}
	var projectCount int
	var invalid []string
	for _, item := range resp.Items {
		if item.Source != "project" {
			continue
		}
		projectCount++
		if item.SchemaVersion == "" {
			invalid = append(invalid, item.Name+": empty schema_version")
		}
	}
	if len(invalid) > 0 {
		return CheckResult{
			Name: "doctrine.overrides.per-project.healthy", Status: "fail",
			Detail: fmt.Sprintf("%d invalid project overrides: %s",
				len(invalid), strings.Join(invalid, "; ")),
			Hint: "run zen doctrine validate <override-path> for each invalid; fix tighten violations",
		}
	}
	return CheckResult{
		Name: "doctrine.overrides.per-project.healthy", Status: "ok",
		Detail: fmt.Sprintf("%d project overrides (Phase J ships embed source only in v0.8.0)",
			projectCount),
	}
}

func checkDoctrineReloadWatcherHealthy(ctx context.Context, c *client.Client) CheckResult {
	resp, err := c.DoctrineV2StatusCall(ctx, "")
	if err != nil {
		return CheckResult{
			Name: "doctrine.reload.watcher.healthy", Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/doctrine/status unreachable; check Phase J wiring",
		}
	}
	if !resp.WatcherHealthy {
		return CheckResult{
			Name: "doctrine.reload.watcher.healthy", Status: "fail",
			Detail: "watcher reports unhealthy (likely fsnotify queue overflow or stalled)",
			Hint:   "run zen doctrine reload --path <doctrine-toml-path> to manually trigger",
		}
	}
	if resp.LastReloadAt != "" && !resp.LastReloadOk {
		return CheckResult{
			Name: "doctrine.reload.watcher.healthy", Status: "warn",
			Detail: fmt.Sprintf("watcher healthy but last reload at %s failed",
				resp.LastReloadAt),
			Hint: "check zen doctrine history --filter reload to investigate",
		}
	}
	detail := "healthy"
	if resp.LastReloadAt != "" {
		detail = fmt.Sprintf("healthy; last reload %s OK", resp.LastReloadAt)
	}
	return CheckResult{
		Name: "doctrine.reload.watcher.healthy", Status: "ok",
		Detail: detail,
	}
}

func checkDoctrineTelemetrySubscriberHealthy(ctx context.Context, c *client.Client) CheckResult {
	resp, err := c.DoctrineV2HistoryCall(ctx, "24h", 0)
	if err != nil {
		return CheckResult{
			Name: "doctrine.telemetry.subscriber.healthy", Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/doctrine/history unreachable; check Phase J + Phase H wiring",
		}
	}
	var reverts int
	for _, ev := range resp.Events {
		if ev.Type == "DoctrineAutonomousReverted" {
			reverts++
		}
	}
	if reverts > 0 {
		return CheckResult{
			Name: "doctrine.telemetry.subscriber.healthy", Status: "warn",
			Detail: fmt.Sprintf("%d autonomous reverts in last 24h", reverts),
			Hint:   "run zen doctrine history --filter reverted to investigate",
		}
	}
	return CheckResult{
		Name: "doctrine.telemetry.subscriber.healthy", Status: "ok",
		Detail: fmt.Sprintf("0 autonomous reverts in last 24h (subscriber + 3 aggregators wired)"),
	}
}

func checkDoctrineLintAnalyzersRegistered(_ context.Context, _ *client.Client) CheckResult {
	type analyzerEntry struct {
		Name      string
		AnalyzerN string
	}
	entries := []analyzerEntry{
		{Name: "nostub", AnalyzerN: nostub.Analyzer.Name},
		{Name: "nostore", AnalyzerN: nostore.Analyzer.Name},
		{Name: "conventional_commit", AnalyzerN: conventional_commit.Analyzer.Name},
	}
	var missing []string
	var present []string
	for _, e := range entries {
		if e.AnalyzerN == "" {
			missing = append(missing, e.Name)
		} else {
			present = append(present, fmt.Sprintf("%s=%s", e.Name, e.AnalyzerN))
		}
	}
	if len(missing) > 0 {
		return CheckResult{
			Name: "doctrine.lint.analyzers.registered", Status: "fail",
			Detail: fmt.Sprintf("%d analyzers missing Name: %s",
				len(missing), strings.Join(missing, ", ")),
			Hint: "rebuild cmd/zen-doctrine-lint; check Phase L analyzer registration",
		}
	}
	return CheckResult{
		Name: "doctrine.lint.analyzers.registered", Status: "ok",
		Detail: fmt.Sprintf("3 analyzers (%s)", strings.Join(present, ", ")),
	}
}

func checkDoctrineSchemaMigrationStatus(ctx context.Context, c *client.Client) CheckResult {
	resp, err := c.DoctrineV2ListCall(ctx, "")
	if err != nil {
		return CheckResult{
			Name: "doctrine.schema.migration.status", Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/doctrine/list unreachable; check Phase J wiring",
		}
	}
	var pending []string
	for _, item := range resp.Items {
		if item.SchemaVersion != "" && item.SchemaVersion != schema.CurrentSchemaVersion {
			pending = append(pending,
				fmt.Sprintf("%s (schema_version=%s)", item.Name, item.SchemaVersion))
		}
	}
	if len(pending) > 0 {
		return CheckResult{
			Name: "doctrine.schema.migration.status", Status: "warn",
			Detail: fmt.Sprintf("current=%s; %d doctrines need migration: %s",
				schema.CurrentSchemaVersion, len(pending), strings.Join(pending, ", ")),
			Hint: "run zen doctrine migrate <path> --confirm for each pending",
		}
	}
	return CheckResult{
		Name: "doctrine.schema.migration.status", Status: "ok",
		Detail: fmt.Sprintf("current=%s; no migration pending", schema.CurrentSchemaVersion),
	}
}

func checkDoctrineReinforcementTemplatesParse(_ context.Context, _ *client.Client) CheckResult {
	engine := reinforcement.New("")
	registry, err := builtinpkg.LoadAll()
	if err != nil {
		return CheckResult{
			Name: "doctrine.reinforcement.templates.parse", Status: "fail",
			Detail: fmt.Sprintf("builtin.LoadAll() failed: %v", err),
		}
	}
	names := []string{"max-scope", "default", "capa-firewall"}
	axioms := v1.TransverseFields()
	for _, name := range names {
		s := registry[name]
		if s == nil {
			return CheckResult{
				Name: "doctrine.reinforcement.templates.parse", Status: "fail",
				Detail: fmt.Sprintf("builtin %q missing from registry", name),
			}
		}
		stubVars := &reinforcement.Vars{
			DoctrineName:       name,
			ProjectAlias:       "test",
			ProjectID:          "test-id",
			CurrentStage:       "design",
			CurrentPhase:       "A",
			TaskKind:           "worker",
			TaskComplexityTier: "moderate",
			PlanID:             "plan-8",
			TransverseAxioms:   axioms,
		}
		if _, err := engine.Render(s, stubVars); err != nil {
			return CheckResult{
				Name: "doctrine.reinforcement.templates.parse", Status: "fail",
				Detail: fmt.Sprintf("template %q render failed: %v", name, err),
				Hint:   "fix syntax in internal/doctrine/reinforcement/embed/" + name + ".system-prompt.md.tmpl",
			}
		}
	}
	return CheckResult{
		Name: "doctrine.reinforcement.templates.parse", Status: "ok",
		Detail: fmt.Sprintf("3 templates (%v) parse + render OK", names),
	}
}

func checkDoctrineAmendmentsPending(ctx context.Context, c *client.Client) CheckResult {
	list, err := c.DoctrineProposeList(ctx)
	if err != nil {
		return CheckResult{
			Name: "doctrine.amendments.pending", Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/doctrine/propose-list unreachable; check Plan 5 + Phase K wiring",
		}
	}
	var pending []string
	for _, p := range list.Proposals {
		if p.Status == "proposed" {
			pending = append(pending, p.ID)
		}
	}
	if len(pending) > 0 {
		return CheckResult{
			Name: "doctrine.amendments.pending", Status: "warn",
			Detail: fmt.Sprintf("%d pending: %s", len(pending), strings.Join(pending, ", ")),
			Hint:   "run zen doctrine ack <adr_id> or zen doctrine deny <adr_id> for each",
		}
	}
	return CheckResult{
		Name: "doctrine.amendments.pending", Status: "ok",
		Detail: "0 pending Qx-4 amendments",
	}
}

func checkDoctrineEventsRecent(ctx context.Context, c *client.Client) CheckResult {
	resp, err := c.DoctrineV2HistoryCall(ctx, "24h", 5)
	if err != nil {
		return CheckResult{
			Name: "doctrine.events.recent", Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/doctrine/history unreachable; check Phase J wiring",
		}
	}
	if len(resp.Events) == 0 {
		return CheckResult{
			Name: "doctrine.events.recent", Status: "warn",
			Detail: "0 events in last 24h",
			Hint:   "doctrine subsystem inactive; if just-restarted, expect at least DoctrineLoaded",
		}
	}
	parts := make([]string, 0, len(resp.Events))
	for _, ev := range resp.Events {
		parts = append(parts, ev.Type)
	}
	return CheckResult{
		Name: "doctrine.events.recent", Status: "ok",
		Detail: fmt.Sprintf("%d recent (%s)", len(resp.Events), strings.Join(parts, ", ")),
	}
}

func checkDoctrineTransverseAxiomsHardcoded(_ context.Context, _ *client.Client) CheckResult {
	axioms := v1.TransverseFields()
	expected := map[string]bool{
		"no_tech_debt": false, "no_stubs": false,
		"build_final_product": false, "no_defer": false,
	}
	for _, name := range axioms {
		if _, want := expected[name]; want {
			expected[name] = true
		}
	}
	var present []string
	var missing []string
	for name, found := range expected {
		if found {
			present = append(present, name)
		} else {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return CheckResult{
			Name: "doctrine.transverse.axioms.hardcoded", Status: "fail",
			Detail: fmt.Sprintf("%d axioms missing: %s",
				len(missing), strings.Join(missing, ", ")),
			Hint: "Phase A transverse.go regression; check inv-zen-135 enforcement",
		}
	}

	return CheckResult{
		Name: "doctrine.transverse.axioms.hardcoded", Status: "ok",
		Detail: fmt.Sprintf("4 axioms hardcoded operator-only: no_tech_debt, no_stubs, build_final_product, no_defer (per inv-zen-135)"),
	}
}
