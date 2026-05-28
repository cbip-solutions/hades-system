// SPDX-License-Identifier: MIT
// Package merge: — task
//
// anomaly.go ships the Severity enum + AnomalyDetector struct + the
// AnomalyDetectedPayload shape + the OnEvent dispatch shell. Per-type
// rolling-window evaluators ship as no-op stubs in C-3 and are filled
// in C-4 (FlakeRate + ModeDegradation) and C-5 (ScoringWinnerVetoed +
// BaselineUnstable + TextualUnresolvable). The skeleton is load-bearing
// for engine.go which constructs the detector and forwards
// events from the eventlog goroutine.
//
// Drift-D structural enforcement (re-stated to keep it visible at the
// file level): there is exactly ONE EventType for anomaly emission —
// EvtMergeAnomalyDetected — and the AnomalyType discriminator lives in
// the payload (AnomalyDetectedPayload.Type). HADES design amendment.proposer
// subscribes to EvtMergeAnomalyDetected, decodes the payload, and
// switches on payload.Type for per-template ADR dispatch.

package merge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Severity is the closed enum of anomaly severity levels. invariant
// sibling: must be Go enum (int kind) so the dispatch table compiles
// (compile-time switch, not a runtime string lookup). Ordered ascending
// (Info < Warning < High < Critical) so callers can compare via simple
// integer ordering when filtering "≥ High" thresholds.
type Severity int

const (
	SeverityUnknown Severity = iota

	SeverityInfo

	SeverityWarning

	SeverityHigh

	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "Info"
	case SeverityWarning:
		return "Warning"
	case SeverityHigh:
		return "High"
	case SeverityCritical:
		return "Critical"
	default:
		return "Unknown"
	}
}

func AllSeverities() []Severity {
	return []Severity{
		SeverityInfo,
		SeverityWarning,
		SeverityHigh,
		SeverityCritical,
	}
}

type AnomalyDetectedPayload struct {
	Type            AnomalyType    `json:"type"`
	Severity        Severity       `json:"severity"`
	ThresholdBreach string         `json:"threshold_breach"`
	Evidence        map[string]any `json:"evidence"`
	Detail          string         `json:"detail"`
	GenerationID    int64          `json:"generation_id,omitempty"`
}

type AnomalyDeps struct {
	Emitter EventEmitter
	Clock   AnomalyClock
	GenCtr  *GenerationCounter
}

type AnomalyClock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type AnomalyThresholds struct {
	ScoringWinnerVetoedCount       int           `toml:"scoring_winner_vetoed_count"`
	ScoringWinnerVetoedWindowHours time.Duration `toml:"scoring_winner_vetoed_window_hours"`

	BaselineUnstableMinDivergentTests int `toml:"baseline_unstable_min_divergent_tests"`

	FlakeRateThresholdPct   float64 `toml:"flake_rate_threshold_pct"`
	FlakeRateWindowSessions int     `toml:"flake_rate_window_sessions"`

	TextualUnresolvableRatePct        float64 `toml:"textual_unresolvable_rate_pct"`
	TextualUnresolvableWindowSessions int     `toml:"textual_unresolvable_window_sessions"`

	ModeDegradationPctThreshold float64       `toml:"mode_degradation_pct_threshold"`
	ModeDegradationWindowHours  time.Duration `toml:"mode_degradation_window_hours"`
}

func DefaultAnomalyThresholds() AnomalyThresholds {
	return AnomalyThresholds{
		ScoringWinnerVetoedCount:          1,
		ScoringWinnerVetoedWindowHours:    24 * time.Hour,
		BaselineUnstableMinDivergentTests: 1,
		FlakeRateThresholdPct:             5.0,
		FlakeRateWindowSessions:           100,
		TextualUnresolvableRatePct:        10.0,
		TextualUnresolvableWindowSessions: 100,
		ModeDegradationPctThreshold:       40.0,
		ModeDegradationWindowHours:        24 * time.Hour,
	}
}

type flakeWindowEntry struct {
	at     time.Time
	flaked bool
}

type modeWindowEntry struct {
	at       time.Time
	degraded bool
}

type AnomalyDetector struct {
	deps       AnomalyDeps
	thresholds AnomalyThresholds
	clock      AnomalyClock

	mu sync.Mutex

	scoringVetoes []time.Time
	flakeSessions []flakeWindowEntry
	unresolvable  []bool
	modeSessions  []modeWindowEntry
	baselineSeen  map[string]map[string]struct{}
}

func NewAnomalyDetector(deps AnomalyDeps, thresholds AnomalyThresholds) (*AnomalyDetector, error) {
	if deps.Emitter == nil {
		return nil, fmt.Errorf("merge.NewAnomalyDetector: Emitter nil")
	}
	clk := deps.Clock
	if clk == nil {
		clk = realClock{}
	}
	return &AnomalyDetector{
		deps:         deps,
		thresholds:   thresholds,
		clock:        clk,
		baselineSeen: make(map[string]map[string]struct{}),
	}, nil
}

func (d *AnomalyDetector) OnEvent(ctx context.Context, evt Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch evt.Type {
	case EvtScoringComplete:
		return d.evalScoringWinnerVetoed(ctx, evt)
	case EvtBaselineComplete:
		return d.evalBaselineUnstable(ctx, evt)
	case EvtCandidateComplete:
		return d.evalFlakeRate(ctx, evt)
	case EvtCandidateFailed:
		return d.evalTextualUnresolvable(ctx, evt)
	case EvtMergeStartedWithMode:
		return d.evalModeDegradation(ctx, evt)
	default:
		return nil
	}
}

func (d *AnomalyDetector) evalScoringWinnerVetoed(ctx context.Context, evt Event) error {
	var p ScoringCompletePayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return nil
	}
	if !p.OperatorVetoed {
		return nil
	}
	now := d.clock.Now()
	cutoff := now.Add(-d.thresholds.ScoringWinnerVetoedWindowHours)

	d.mu.Lock()
	d.scoringVetoes = append(d.scoringVetoes, now)

	trimmed := d.scoringVetoes[:0]
	for _, t := range d.scoringVetoes {
		if t.After(cutoff) {
			trimmed = append(trimmed, t)
		}
	}
	d.scoringVetoes = trimmed
	count := len(d.scoringVetoes)
	d.mu.Unlock()

	if count < d.thresholds.ScoringWinnerVetoedCount {
		return nil
	}
	d.emit(ctx,
		AnomalyScoringFormulaWinnerVetoed,
		SeverityHigh,
		fmt.Sprintf("vetoes %d ≥ %d in %s", count, d.thresholds.ScoringWinnerVetoedCount, d.thresholds.ScoringWinnerVetoedWindowHours),
		map[string]any{
			"vetoes_in_window": count,
			"threshold":        d.thresholds.ScoringWinnerVetoedCount,
			"window_hours":     d.thresholds.ScoringWinnerVetoedWindowHours.Hours(),
		},
		"operator vetoes recurring; scoring weight tuning evaluation suggested",
	)
	return nil
}

func (d *AnomalyDetector) evalBaselineUnstable(ctx context.Context, evt Event) error {
	var p BaselineCompletePayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return nil
	}

	d.mu.Lock()
	if d.baselineSeen[p.BaseSHA] == nil {
		d.baselineSeen[p.BaseSHA] = make(map[string]struct{})
	}
	d.baselineSeen[p.BaseSHA][p.PassingSetHash] = struct{}{}
	divergent := len(d.baselineSeen[p.BaseSHA])
	d.mu.Unlock()

	if divergent < 2 {
		return nil
	}
	if divergent < d.thresholds.BaselineUnstableMinDivergentTests+1 {
		return nil
	}
	d.emit(ctx,
		AnomalyBaselineUnstableAcrossSessions,
		SeverityHigh,
		fmt.Sprintf("base_sha %s: %d distinct passing_set hashes", p.BaseSHA, divergent),
		map[string]any{
			"base_sha":        p.BaseSHA,
			"distinct_hashes": divergent,
		},
		"baseline non-determinism observed across sessions; test-infra flakiness investigation suggested",
	)
	return nil
}

func (d *AnomalyDetector) evalTextualUnresolvable(ctx context.Context, evt Event) error {
	var p CandidateFailedPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return nil
	}

	d.mu.Lock()
	d.unresolvable = append(d.unresolvable, p.FailureType == CandidateFailurePatchRejected.String())

	if len(d.unresolvable) > d.thresholds.TextualUnresolvableWindowSessions {
		d.unresolvable = d.unresolvable[len(d.unresolvable)-d.thresholds.TextualUnresolvableWindowSessions:]
	}
	rejN := 0
	for _, b := range d.unresolvable {
		if b {
			rejN++
		}
	}
	total := len(d.unresolvable)
	d.mu.Unlock()

	if total < d.thresholds.TextualUnresolvableWindowSessions {
		return nil
	}
	rate := float64(rejN) / float64(total) * 100.0
	if rate <= d.thresholds.TextualUnresolvableRatePct {
		return nil
	}
	d.emit(ctx,
		AnomalyTextualMergeUnresolvableRateHigh,
		SeverityHigh,
		fmt.Sprintf("rate %.2f%% > %.2f%%", rate, d.thresholds.TextualUnresolvableRatePct),
		map[string]any{
			"rate":               rate,
			"window_sessions":    total,
			"unresolvable_count": rejN,
		},
		"textual merge consistently unresolvable; ADR-0035 (AST-aware merge) evaluation suggested",
	)
	return nil
}

func (d *AnomalyDetector) evalFlakeRate(ctx context.Context, evt Event) error {
	var p CandidateCompletePayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return nil
	}

	d.mu.Lock()
	d.flakeSessions = append(d.flakeSessions, flakeWindowEntry{
		at:     d.clock.Now(),
		flaked: p.FlakeCount > 0,
	})

	if len(d.flakeSessions) > d.thresholds.FlakeRateWindowSessions {
		d.flakeSessions = d.flakeSessions[len(d.flakeSessions)-d.thresholds.FlakeRateWindowSessions:]
	}
	flakedN := 0
	for _, e := range d.flakeSessions {
		if e.flaked {
			flakedN++
		}
	}
	total := len(d.flakeSessions)
	d.mu.Unlock()

	if total < d.thresholds.FlakeRateWindowSessions {
		return nil
	}
	rate := float64(flakedN) / float64(total) * 100.0
	if rate <= d.thresholds.FlakeRateThresholdPct {
		return nil
	}
	d.emit(ctx,
		AnomalyFlakeRateAboveThreshold,
		SeverityHigh,
		fmt.Sprintf("flake_rate %.2f%% > %.2f%%", rate, d.thresholds.FlakeRateThresholdPct),
		map[string]any{
			"rate":            rate,
			"window_sessions": total,
			"flaked_count":    flakedN,
		},
		"γ scoring penalty consistently activated; flake-hardening evaluation suggested",
	)
	return nil
}

// evalModeDegradation aggregates EvtMergeStartedWithMode events into a
// sliding TIME window (eviction by clock-derived cutoff). Each event whose
// MergeStartedWithModePayload.Mode != "Normal" counts as one degraded entry.
//
// Eviction policy: every call recomputes cutoff = now - ModeDegradationWindowHours
// and drops entries with at <= cutoff (keep only e.at.After(cutoff)). The
// in-place trim using `d.modeSessions[:0]` reuses the underlying array so
// long-running detectors do not accumulate slice-header growth.
//
// Saturation guard: total < 5 → return nil. Below five sessions the pct
// statistic is too noisy to drive an amendment-flow signal (one Degraded
// in 1 sample = 100% which is meaningless). The "5" threshold is doctrine-
// implicit (matches §7 spec "≥5 for meaningful aggregation").
//
// On pct > ModeDegradationPctThreshold, emits AnomalyModeDegradationPersistent
// with SeverityWarning + Evidence{degraded_pct, window_sessions, degraded_count}.
// Severity is Warning (not High) per design contract: degradation pattern → suggested
// cost-budget tightening, not immediate operator action.
func (d *AnomalyDetector) evalModeDegradation(ctx context.Context, evt Event) error {
	var p MergeStartedWithModePayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return nil
	}
	now := d.clock.Now()
	cutoff := now.Add(-d.thresholds.ModeDegradationWindowHours)

	d.mu.Lock()
	d.modeSessions = append(d.modeSessions, modeWindowEntry{
		at:       now,
		degraded: p.Mode != "Normal",
	})

	trimmed := d.modeSessions[:0]
	for _, e := range d.modeSessions {
		if e.at.After(cutoff) {
			trimmed = append(trimmed, e)
		}
	}
	d.modeSessions = trimmed
	degradedN := 0
	for _, e := range d.modeSessions {
		if e.degraded {
			degradedN++
		}
	}
	total := len(d.modeSessions)
	d.mu.Unlock()

	if total < 5 {
		return nil
	}
	pct := float64(degradedN) / float64(total) * 100.0
	if pct <= d.thresholds.ModeDegradationPctThreshold {
		return nil
	}
	d.emit(ctx,
		AnomalyModeDegradationPersistent,
		SeverityWarning,
		fmt.Sprintf("degraded_pct %.2f%% > %.2f%%", pct, d.thresholds.ModeDegradationPctThreshold),
		map[string]any{
			"degraded_pct":    pct,
			"window_sessions": total,
			"degraded_count":  degradedN,
		},
		"sessions consistently below Normal mode; doctrine cost-budget tightening evaluation suggested",
	)
	return nil
}

func (d *AnomalyDetector) emit(ctx context.Context, anomalyType AnomalyType, sev Severity, breach string, evidence map[string]any, detail string) {
	gen := int64(0)
	if d.deps.GenCtr != nil {
		gen = d.deps.GenCtr.Current()
	}
	payload, _ := json.Marshal(AnomalyDetectedPayload{
		Type:            anomalyType,
		Severity:        sev,
		ThresholdBreach: breach,
		Evidence:        evidence,
		Detail:          detail,
		GenerationID:    gen,
	})
	_ = d.deps.Emitter.Append(ctx, Event{
		Type:         EvtMergeAnomalyDetected,
		GenerationID: gen,
		Payload:      payload,
		Timestamp:    d.clock.Now(),
	})
}

// HADES component is the first ADR ID reserved anomaly proposals.
// invariant: every ADR proposed in response to a HADES design EvtMergeAnomalyDetected
// event MUST allocate its ID from [HADES component, HADES component]. HADES design
// amendment.proposer enforces at allocation time ( cross-branch
// amendment); the compliance test verifies the reserved range constants are
// present + correct here at the merge package surface.
const Plan6ADRRangeStart = 30

const Plan6ADRRangeEnd = 39

func Plan6ADRRange() (start, end int) {
	return Plan6ADRRangeStart, Plan6ADRRangeEnd
}
