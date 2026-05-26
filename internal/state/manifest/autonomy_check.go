// SPDX-License-Identifier: MIT
package manifest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const ChainIntegrityFreshThreshold = 24 * time.Hour

type PrereqProbes interface {
	ChainIntegrityFresh(ctx context.Context, threshold time.Duration) (ok bool, reason string, err error)

	BackupHealthy(ctx context.Context) (ok bool, reason string, err error)

	KnowledgeAggregatorReady(ctx context.Context) (ok bool, reason string, err error)

	ResearchCacheReady(ctx context.Context) (ok bool, reason string, err error)

	WitnessKeyValid(ctx context.Context) (ok bool, reason string, err error)

	ADRsValid(ctx context.Context) (ok bool, reason string, err error)
}

type AutonomyResult struct {
	Check string

	Pass bool

	Reason string
}

type AutonomyReport struct {
	AllPass bool

	Results []AutonomyResult

	Failures []AutonomyResult
}

type Plan9PrereqInputs struct {
	StatePath string

	Now time.Time

	RecentEvents []ChainAnchoredEvent

	Probes PrereqProbes
}

type AutonomyValidator struct {
	differ *Differ
	schema *Schema
}

func NewAutonomyValidator(d *Differ, s *Schema) *AutonomyValidator {
	if d == nil {
		panic("manifest.NewAutonomyValidator: nil differ")
	}
	if s == nil {
		panic("manifest.NewAutonomyValidator: nil schema")
	}
	return &AutonomyValidator{differ: d, schema: s}
}

func (v *AutonomyValidator) ValidateStateFreshness(
	ctx context.Context,
	manifestPath string,
	now time.Time,
	recentEvents []ChainAnchoredEvent,
) (AutonomyResult, error) {
	const checkName = "state.freshness"

	body, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return AutonomyResult{
				Check:  checkName,
				Pass:   false,
				Reason: fmt.Sprintf("docs/system-state.toml not found at %s; run `zen state regenerate`", manifestPath),
			}, nil
		}
		return AutonomyResult{Check: checkName, Pass: false, Reason: "read error: " + err.Error()}, nil
	}

	var m Manifest
	if _, decErr := toml.NewDecoder(bytes.NewReader(body)).Decode(&m); decErr != nil {
		return AutonomyResult{
			Check:  checkName,
			Pass:   false,
			Reason: "docs/system-state.toml failed TOML decode; restore from VCS or run `zen state regenerate`",
		}, nil
	}

	age := now.Sub(m.Provenance.LastRegenerate)
	if age <= FreshnessThreshold {
		return AutonomyResult{Check: checkName, Pass: true}, nil
	}

	if hasCompensatingEvent(recentEvents, now) {
		return AutonomyResult{Check: checkName, Pass: true}, nil
	}

	return AutonomyResult{
		Check: checkName,
		Pass:  false,
		Reason: fmt.Sprintf(
			"docs/system-state.toml is %s old (threshold %s) without recent state.manual_field_changed event; run `zen state regenerate` or pin a manual field to extend the freshness lease",
			age.Truncate(time.Hour),
			FreshnessThreshold,
		),
	}, nil
}

func (v *AutonomyValidator) ValidateAll(ctx context.Context, in Plan9PrereqInputs) AutonomyReport {
	var results []AutonomyResult

	add := func(r AutonomyResult) {
		results = append(results, r)
	}

	r1, _ := v.ValidateStateFreshness(ctx, in.StatePath, in.Now, in.RecentEvents)
	add(r1)

	add(autonomyFromProbeThreshold(ctx, "chain.integrity", in.Probes.ChainIntegrityFresh, ChainIntegrityFreshThreshold))

	add(autonomyFromProbe(ctx, "backup.healthy", in.Probes.BackupHealthy))

	add(autonomyFromProbe(ctx, "knowledge.ready", in.Probes.KnowledgeAggregatorReady))

	add(autonomyFromProbe(ctx, "research.ready", in.Probes.ResearchCacheReady))

	add(autonomyFromProbe(ctx, "witness.key", in.Probes.WitnessKeyValid))

	add(autonomyFromProbe(ctx, "adrs.valid", in.Probes.ADRsValid))

	report := AutonomyReport{Results: results, AllPass: true}
	for _, res := range results {
		if !res.Pass {
			report.AllPass = false
			report.Failures = append(report.Failures, res)
		}
	}
	return report
}

func FormatAutonomyReport(r AutonomyReport) string {
	var b strings.Builder
	if r.AllPass {
		b.WriteString("Plan 9 prereqs: ALL PASS\n")
	} else {
		fmt.Fprintf(&b, "Plan 9 prereqs: %d/%d FAIL\n", len(r.Failures), len(r.Results))
	}
	for _, res := range r.Results {
		mark := "PASS"
		if !res.Pass {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "  [%s] %s", mark, res.Check)
		if res.Reason != "" {
			fmt.Fprintf(&b, ": %s", res.Reason)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func autonomyFromProbe(
	ctx context.Context,
	checkName string,
	fn func(ctx context.Context) (bool, string, error),
) AutonomyResult {
	ok, reason, err := safeCallProbe(ctx, fn)
	if err != nil {
		return AutonomyResult{Check: checkName, Pass: false, Reason: "probe error: " + err.Error()}
	}
	if !ok {
		return AutonomyResult{Check: checkName, Pass: false, Reason: reason}
	}
	return AutonomyResult{Check: checkName, Pass: true}
}

func autonomyFromProbeThreshold(
	ctx context.Context,
	checkName string,
	fn func(ctx context.Context, threshold time.Duration) (bool, string, error),
	threshold time.Duration,
) AutonomyResult {
	wrapped := func(ctx context.Context) (bool, string, error) {
		return fn(ctx, threshold)
	}
	return autonomyFromProbe(ctx, checkName, wrapped)
}

func safeCallProbe(ctx context.Context, fn func(context.Context) (bool, string, error)) (ok bool, reason string, err error) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			reason = ""
			err = fmt.Errorf("probe panic: %v", r)
		}
	}()
	return fn(ctx)
}
