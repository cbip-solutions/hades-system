// SPDX-License-Identifier: MIT
package aggregator

import (
	"sync"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type FailureClass string

const (
	FailureClassTransientLLM FailureClass = "TRANSIENT_LLM"

	FailureClassTransientInfra FailureClass = "TRANSIENT_INFRA"

	FailureClassPermanentTask FailureClass = "PERMANENT_TASK"

	FailureClassPermanentInfra FailureClass = "PERMANENT_INFRA"
)

func AllFailureClasses() []FailureClass {
	return []FailureClass{
		FailureClassTransientLLM, FailureClassTransientInfra,
		FailureClassPermanentTask, FailureClassPermanentInfra,
	}
}

type Recovery struct {
	mu     sync.RWMutex
	rules  map[string]RuleSpec
	states map[string]*WindowState
}

func NewRecovery() *Recovery {
	return &Recovery{
		rules:  make(map[string]RuleSpec),
		states: make(map[string]*WindowState),
	}
}

func (r *Recovery) RegisterRule(spec RuleSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules[spec.RulePath] = spec
	if _, ok := r.states[spec.RulePath]; !ok {
		r.states[spec.RulePath] = NewWindowState(spec.WindowSessions)
	}
}

func (r *Recovery) RecordSession(ctx SessionContext) {
	r.mu.RLock()
	rules := make([]RuleSpec, 0, len(r.rules))
	for _, spec := range r.rules {
		rules = append(rules, spec)
	}
	r.mu.RUnlock()

	for _, spec := range rules {
		anomalous := sessionMatchesAnomalousEvents(ctx.EventsThisSession, spec.AnomalousEvents, spec.PayloadFilter)
		r.mu.RLock()
		w := r.states[spec.RulePath]
		r.mu.RUnlock()

		w.Record(SessionRecord{
			SessionID: ctx.SessionID,
			Anomaly:   anomalous,
			Timestamp: ctx.Timestamp,
			SourceADR: ctx.LastAppliedADR,
		})
	}
}

func (r *Recovery) Evaluate(rulePath string, requestedWindow int) (pctPassing float64, totalSessions int, lastApplied string) {
	r.mu.RLock()
	w := r.states[rulePath]
	r.mu.RUnlock()
	if w == nil {
		return 1.0, 0, ""
	}
	return w.Evaluate(requestedWindow)
}

func (r *Recovery) RuleSpecs() []RuleSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RuleSpec, 0, len(r.rules))
	for _, spec := range r.rules {
		out = append(out, spec)
	}
	return out
}

func DefaultRecoveryAnomalyEventTypes() []eventlog.EventType {
	return []eventlog.EventType{
		eventlog.EvtWorkerDeath,
		eventlog.EvtWorkerRedispatched,
		eventlog.EvtEscalationDecision,
		eventlog.EvtArchitecturalReview,
	}
}

func WorkerFailureClassFilter(classes ...FailureClass) PayloadPredicate {
	allowed := make(map[FailureClass]bool, len(classes))
	for _, c := range classes {
		allowed[c] = true
	}
	return func(ev eventlog.PayloadEncoder) bool {
		switch e := ev.(type) {
		case eventlog.WorkerDeath:
			return allowed[FailureClass(e.Class)]
		case eventlog.WorkerRedispatched:
			return allowed[FailureClass(e.Class)]
		default:

			return true
		}
	}
}
