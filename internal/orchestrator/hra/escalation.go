// SPDX-License-Identifier: MIT
package hra

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type EscalationRules struct {
	log       EventLog
	clk       clock.Clock
	sessionID string
	projectID string

	l3Escalations atomic.Int64
	l3Acks        atomic.Int64

	mu sync.Mutex
}

// NewEscalationRules constructs an EscalationRules. All deps are
// required; the coordinator wires this via Run's default-escalator
// path (see HRACoordinator.Run + SetEscalator).
//
// The sessionID/projectID pair is captured at construction so emitted
// rows always reflect the orchestrator's identity at coordinator
// startup — these MUST match the coordinator's CoordinatorContext to
// avoid split-attribution between the H-4 emitEscalation row and the
// H-6 EscalationRules-shaped row.
func NewEscalationRules(log EventLog, clk clock.Clock, sessionID, projectID string) *EscalationRules {
	return &EscalationRules{
		log:       log,
		clk:       clk,
		sessionID: sessionID,
		projectID: projectID,
	}
}

// HandleDisagreement is invoked by a cadence goroutine when its
// per-layer aggregator emits a Finding with Disagreement=true OR
// (architectural-only) NeedsFix=true. Emits a single
// EvtEscalationDecision row with the routed from/to/class triple per
// spec §1 Q6 D.
//
// L1 (worker) and any unknown Layer values silently no-op — workers
// aren't reviewers and have no escalation target in this chain.
//
// Strategic-layer escalations bump the L3 escalation counter that
// H-7's deadlock detector reads via L3Counters. Tactical and
// architectural escalations DO NOT bump that counter (only the L3
// strategic-layer ratio is load-bearing for deadlock detection).
//
// Append uses context.WithoutCancel(context.Background()) so audit
// emission is decoupled from any per-call cancellation context — a
// tick that fires concurrent with Run-context cancellation must still
// durable-write the escalation it observed before cancel landed
// (matches the D-2/D-3/E-2/F-2/F-3/G-1/H-1..H-5 audit-trail discipline).
func (r *EscalationRules) HandleDisagreement(layer Layer, f Finding) {
	from, to, class := r.routeEscalation(layer)
	if from == "" {
		return
	}

	if layer == LayerStrategic {
		r.l3Escalations.Add(1)

		if r.IsL3Deadlock(L3DeadlockThreshold) {
			class = "strategic_persistent_deadlock"
		}
	}

	payload := map[string]any{
		"from":         from,
		"to":           to,
		"class":        class,
		"verdict":      f.Verdict,
		"disagreement": f.Disagreement,
		"needs_fix":    f.NeedsFix,

		"split": []int{f.Split[0], f.Split[1]},
	}
	if f.Summary != "" {
		payload["summary"] = f.Summary
	}
	if len(f.FixProposals) > 0 {
		payload["fix_proposals"] = f.FixProposals
	}

	auditCtx := context.WithoutCancel(context.Background())
	_, _ = r.log.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtEscalationDecision,
		SessionID: r.sessionID,
		ProjectID: r.projectID,
		Timestamp: r.clk.Now(),
		Payload:   payload,
	})
}

func (r *EscalationRules) HighBlastRadius(ctx context.Context, level string, score float64, topAffected []string) {
	if level != "high" {
		return
	}
	summary := "high blast-radius change requires strategic review"
	if len(topAffected) > 0 {
		summary = fmt.Sprintf("high blast-radius change (score %.2f) requires strategic review; top-affected: %v", score, topAffected)
	}
	finding := Finding{
		Layer:        LayerStrategic,
		Verdict:      "needs_fix",
		NeedsFix:     true,
		Disagreement: true,
		Split:        [2]int{0, 1},
		Summary:      summary,
		FixProposals: topAffected,
	}

	_ = ctx
	r.HandleDisagreement(LayerStrategic, finding)
}

func (r *EscalationRules) AckedAtLayer(layer Layer) {
	if layer == LayerStrategic {
		r.l3Acks.Add(1)
	}
}

func (r *EscalationRules) routeEscalation(layer Layer) (from, to, class string) {
	switch layer {
	case LayerTactical:
		return "tactical", "strategic", "tactical_disagreement"
	case LayerStrategic:
		return "strategic", "architectural", "strategic_deadlock"
	case LayerArchitectural:
		return "architectural", "operator", "architectural"
	default:
		return "", "", ""
	}
}

func (r *EscalationRules) L3Counters() (escalations, acks int64, ratio float64) {
	e := r.l3Escalations.Load()
	a := r.l3Acks.Load()
	total := e + a
	if total == 0 {
		return e, a, 0
	}
	return e, a, float64(e) / float64(total)
}

const L3DeadlockThreshold = 0.5

func (r *EscalationRules) IsL3Deadlock(threshold float64) bool {
	esc, _, ratio := r.L3Counters()
	if esc == 0 {
		return false
	}
	return ratio >= threshold
}

func (r *EscalationRules) Cooldown() time.Duration { return 30 * time.Second }
