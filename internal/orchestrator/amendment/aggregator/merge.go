// SPDX-License-Identifier: MIT
package aggregator

import (
	"sync"
)

type Merge struct {
	mu     sync.RWMutex
	rules  map[string]RuleSpec
	states map[string]*WindowState
}

func NewMerge() *Merge {
	return &Merge{
		rules:  make(map[string]RuleSpec),
		states: make(map[string]*WindowState),
	}
}

func (m *Merge) RegisterRule(spec RuleSpec) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[spec.RulePath] = spec
	if _, ok := m.states[spec.RulePath]; !ok {
		m.states[spec.RulePath] = NewWindowState(spec.WindowSessions)
	}
}

func (m *Merge) RecordSession(ctx SessionContext) {
	m.mu.RLock()
	rules := make([]RuleSpec, 0, len(m.rules))
	for _, r := range m.rules {
		rules = append(rules, r)
	}
	m.mu.RUnlock()

	for _, spec := range rules {
		anomalous := sessionMatchesAnomalousEvents(ctx.EventsThisSession, spec.AnomalousEvents, spec.PayloadFilter)
		m.mu.RLock()
		w := m.states[spec.RulePath]
		m.mu.RUnlock()

		w.Record(SessionRecord{
			SessionID: ctx.SessionID,
			Anomaly:   anomalous,
			Timestamp: ctx.Timestamp,
			SourceADR: ctx.LastAppliedADR,
		})
	}
}

func (m *Merge) Evaluate(rulePath string, requestedWindow int) (pctPassing float64, totalSessions int, lastApplied string) {
	m.mu.RLock()
	w := m.states[rulePath]
	m.mu.RUnlock()
	if w == nil {
		return 1.0, 0, ""
	}
	return w.Evaluate(requestedWindow)
}

func (m *Merge) RuleSpecs() []RuleSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RuleSpec, 0, len(m.rules))
	for _, r := range m.rules {
		out = append(out, r)
	}
	return out
}
