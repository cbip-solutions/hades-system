// SPDX-License-Identifier: MIT
package aggregator

import (
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type PayloadPredicate func(eventlog.PayloadEncoder) bool

// RuleSpec is the registration record for a single enforce-tier rule.
// Per HADES design design spec §1 design choice C Tier 2 schema, each rule carries
// revert_category + revert_threshold_pct + revert_window_sessions +
// revert_cooldown_hours metadata; the aggregator uses the
// AnomalousEvents whitelist to decide whether each session's events
// count as anomaly for this rule.
//
// Per self-review NIT #16: RuleSpec is declared ONCE here in
// task with the FINAL field set (including PayloadFilter from H-4).
// Tasks H-3 / H-4 narratives reference but do NOT redeclare RuleSpec
// (avoids the duplicate-declaration trap).
type RuleSpec struct {
	RulePath        string
	WindowSessions  int
	ThresholdPct    float64
	AnomalousEvents []eventlog.EventType
	PayloadFilter   PayloadPredicate
}

type SessionContext struct {
	SessionID         string
	Timestamp         time.Time
	LastAppliedADR    string
	EventsThisSession []eventlog.PayloadEncoder
}

type Cost struct {
	mu     sync.RWMutex
	rules  map[string]RuleSpec
	states map[string]*WindowState
}

func NewCost() *Cost {
	return &Cost{
		rules:  make(map[string]RuleSpec),
		states: make(map[string]*WindowState),
	}
}

func (c *Cost) RegisterRule(spec RuleSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules[spec.RulePath] = spec
	if _, ok := c.states[spec.RulePath]; !ok {
		c.states[spec.RulePath] = NewWindowState(spec.WindowSessions)
	}
}

func (c *Cost) RecordSession(ctx SessionContext) {
	c.mu.RLock()
	rules := make([]RuleSpec, 0, len(c.rules))
	for _, r := range c.rules {
		rules = append(rules, r)
	}
	c.mu.RUnlock()

	for _, spec := range rules {
		anomalous := sessionMatchesAnomalousEvents(ctx.EventsThisSession, spec.AnomalousEvents, spec.PayloadFilter)
		c.mu.RLock()
		w := c.states[spec.RulePath]
		c.mu.RUnlock()

		w.Record(SessionRecord{
			SessionID: ctx.SessionID,
			Anomaly:   anomalous,
			Timestamp: ctx.Timestamp,
			SourceADR: ctx.LastAppliedADR,
		})
	}
}

func (c *Cost) Evaluate(rulePath string, requestedWindow int) (pctPassing float64, totalSessions int, lastApplied string) {
	c.mu.RLock()
	w := c.states[rulePath]
	c.mu.RUnlock()
	if w == nil {
		return 1.0, 0, ""
	}
	return w.Evaluate(requestedWindow)
}

func (c *Cost) RuleSpecs() []RuleSpec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]RuleSpec, 0, len(c.rules))
	for _, r := range c.rules {
		out = append(out, r)
	}
	return out
}

func sessionMatchesAnomalousEvents(events []eventlog.PayloadEncoder, whitelist []eventlog.EventType, predicate PayloadPredicate) bool {
	if len(events) == 0 || len(whitelist) == 0 {
		return false
	}
	for _, ev := range events {
		for _, t := range whitelist {
			if ev.Type() != t {
				continue
			}
			if predicate == nil || predicate(ev) {
				return true
			}
		}
	}
	return false
}
