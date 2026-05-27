// SPDX-License-Identifier: MIT
package amendment

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment/aggregator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type AutoReverter interface {
	AutoRevert(ctx context.Context, adrID int, telemetryReason string) error
}

type TelemetryEmitter interface {
	EmitDoctrineEvent(ctx context.Context, ev eventlog.PayloadEncoder) error
}

type CapaFirewallProbe interface {
	IsCapaFirewall(projectID string) bool
}

type CooldownTracker interface {
	LastRevertedAt(rulePath string) (when time.Time, cooldown time.Duration, ok bool)
	// MarkReverted records a successful revert at `when` with the given
	// cooldown duration; future LastRevertedAt calls within the cooldown
	// MUST return ok=true so TelemetrySubscriber can suppress.
	MarkReverted(rulePath string, when time.Time, cooldown time.Duration)
}

type CooldownPolicy interface {
	CooldownFor(projectID, rulePath string) time.Duration
}

type TelemetrySubscriberConfig struct {
	Cost         *aggregator.Cost
	Merge        *aggregator.Merge
	Recovery     *aggregator.Recovery
	Reverter     AutoReverter
	Emitter      TelemetryEmitter
	CapaFirewall CapaFirewallProbe // optional; nil → no capa-firewall guard (test convenience). Production MUST wire.
	Cooldown     CooldownTracker
	Policy       CooldownPolicy
	Clock        clock.Clock
}

type TelemetrySubscriber struct {
	cfg  TelemetrySubscriberConfig
	mu   sync.Mutex
	last map[string]time.Time
}

func NewTelemetrySubscriber(cfg TelemetrySubscriberConfig) *TelemetrySubscriber {
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	return &TelemetrySubscriber{cfg: cfg, last: map[string]time.Time{}}
}

// EvaluateProject is the test/admin entry point. It walks every
// registered rule across all 3 aggregators, fetches per-rule revert
// metadata from the project's active doctrine schema, and dispatches
// AutoRevert when (a) total_sessions ≥ revert_window_sessions, (b)
// pct_passing < revert_threshold_pct, (c) lastApplied != ""
// , (d) project is NOT bound to capa-firewall
// , and (e) per-rule cooldown is satisfied (invariant,
// Task H-8).
//
// Returns the count of AutoRevert calls dispatched (useful for
// observability / test assertions). Errors during dispatch are
// emitted as DoctrineAmendmentApplyFailed events but do not abort the
// pass — other rules continue to evaluate.
func (t *TelemetrySubscriber) EvaluateProject(ctx context.Context, projectID string) (dispatched int, err error) {

	if t.cfg.CapaFirewall != nil && t.cfg.CapaFirewall.IsCapaFirewall(projectID) {
		return 0, nil
	}

	for _, agg := range t.aggregators() {
		for _, rule := range agg.specs() {
			fired, err := t.evaluateRule(ctx, projectID, agg, rule)
			if err != nil {

				continue
			}
			if fired {
				dispatched++
			}
		}
	}
	return dispatched, nil
}

func (t *TelemetrySubscriber) evaluateRule(ctx context.Context, projectID string, agg aggregatorView, rule aggregator.RuleSpec) (fired bool, err error) {
	pct, total, lastApplied := agg.evaluate(rule.RulePath, rule.WindowSessions)
	if total < rule.WindowSessions {
		return false, nil
	}
	if pct >= rule.ThresholdPct {
		return false, nil
	}
	if lastApplied == "" {
		return false, nil
	}

	now := t.cfg.Clock.Now()
	cooldown := t.cooldownFor(projectID, rule.RulePath)
	if t.cooldownActive(rule.RulePath, now, cooldown) {
		when, _ := t.cooldownLast(rule.RulePath)
		remainingHours := cooldown.Hours() - now.Sub(when).Hours()
		if remainingHours < 0 {
			remainingHours = 0
		}
		_ = t.cfg.Emitter.EmitDoctrineEvent(ctx, eventlog.DoctrineRevertSuppressedCooldown{
			ADRID:                  lastApplied,
			RulePath:               rule.RulePath,
			TelemetryCategory:      agg.category(),
			AttemptedAtUnix:        now.Unix(),
			LastRevertedAtUnix:     when.Unix(),
			CooldownUntil:          when.Add(cooldown),
			CooldownRemainingHours: remainingHours,
			At:                     now.UTC(),
		})
		return false, nil
	}

	reason := fmt.Sprintf("%s aggregator: pct_passing=%.3f below threshold=%.3f over %d sessions",
		agg.category(), pct, rule.ThresholdPct, total)

	adrID, parseErr := parseADRID(lastApplied)
	if parseErr != nil {

		_ = t.cfg.Emitter.EmitDoctrineEvent(ctx, eventlog.DoctrineAmendmentApplyFailed{
			ADRID:  lastApplied,
			Stage:  "auto-revert-parse",
			Reason: parseErr.Error(),
		})
		return false, parseErr
	}

	if err := t.cfg.Reverter.AutoRevert(ctx, adrID, reason); err != nil {
		// Reverter failed; do NOT mark cooldown (operator may want to retry).
		_ = t.cfg.Emitter.EmitDoctrineEvent(ctx, eventlog.DoctrineAmendmentApplyFailed{
			ADRID:  lastApplied,
			Stage:  "auto-revert",
			Reason: err.Error(),
		})
		return false, err
	}
	t.markReverted(rule.RulePath, now, cooldown)

	_ = t.cfg.Emitter.EmitDoctrineEvent(ctx, eventlog.DoctrineAutonomousReverted{
		ADRID:             lastApplied,
		RulePath:          rule.RulePath,
		TelemetryCategory: agg.category(),
		ThresholdBreached: pct,
		WindowSessions:    total,
		Reason:            reason,
	})
	return true, nil
}

type aggregatorView interface {
	specs() []aggregator.RuleSpec
	evaluate(rulePath string, window int) (pctPassing float64, total int, lastApplied string)
	category() string
}

type costView struct{ a *aggregator.Cost }

func (c costView) specs() []aggregator.RuleSpec { return c.a.RuleSpecs() }
func (c costView) evaluate(r string, w int) (float64, int, string) {
	return c.a.Evaluate(r, w)
}
func (c costView) category() string { return "cost" }

type mergeView struct{ a *aggregator.Merge }

func (m mergeView) specs() []aggregator.RuleSpec { return m.a.RuleSpecs() }
func (m mergeView) evaluate(r string, w int) (float64, int, string) {
	return m.a.Evaluate(r, w)
}
func (m mergeView) category() string { return "merge" }

type recoveryView struct{ a *aggregator.Recovery }

func (r recoveryView) specs() []aggregator.RuleSpec { return r.a.RuleSpecs() }
func (r recoveryView) evaluate(rule string, w int) (float64, int, string) {
	return r.a.Evaluate(rule, w)
}
func (r recoveryView) category() string { return "recovery" }

func (t *TelemetrySubscriber) aggregators() []aggregatorView {
	out := make([]aggregatorView, 0, 3)
	if t.cfg.Cost != nil {
		out = append(out, costView{t.cfg.Cost})
	}
	if t.cfg.Merge != nil {
		out = append(out, mergeView{t.cfg.Merge})
	}
	if t.cfg.Recovery != nil {
		out = append(out, recoveryView{t.cfg.Recovery})
	}
	return out
}

func (t *TelemetrySubscriber) cooldownFor(projectID, rulePath string) time.Duration {
	if t.cfg.Policy != nil {
		if d := t.cfg.Policy.CooldownFor(projectID, rulePath); d > 0 {
			return d
		}
	}
	return 24 * time.Hour
}

func (t *TelemetrySubscriber) cooldownActive(rulePath string, now time.Time, cooldown time.Duration) bool {
	when, ok := t.cooldownLast(rulePath)
	if !ok {
		return false
	}
	return now.Sub(when) < cooldown
}

func (t *TelemetrySubscriber) cooldownLast(rulePath string) (time.Time, bool) {
	if t.cfg.Cooldown != nil {
		when, _, ok := t.cfg.Cooldown.LastRevertedAt(rulePath)
		return when, ok
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	when, ok := t.last[rulePath]
	return when, ok
}

func (t *TelemetrySubscriber) markReverted(rulePath string, when time.Time, cooldown time.Duration) {
	if t.cfg.Cooldown != nil {
		t.cfg.Cooldown.MarkReverted(rulePath, when, cooldown)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last[rulePath] = when
}

func parseADRID(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty ADR ID")
	}

	if len(s) > 4 && s[:4] == "ADR-" {
		s = s[4:]
	}
	var n int
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("malformed ADR ID: non-digit %q at offset %d", c, i)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
