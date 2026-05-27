// go:build integration
package plan19

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type n4FakeEventLog struct {
	mu      sync.Mutex
	appends []eventlog.Event
}

func (f *n4FakeEventLog) Subscribe(_ eventlog.Filter, _ int) eventlog.Subscription {

	return nopSubscription{}
}

func (f *n4FakeEventLog) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appends = append(f.appends, ev)
	return int64(len(f.appends)), nil
}

func (f *n4FakeEventLog) Appends() []eventlog.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]eventlog.Event, len(f.appends))
	copy(out, f.appends)
	return out
}

type nopSubscription struct{}

func (nopSubscription) Events() <-chan eventlog.Record { ch := make(chan eventlog.Record); return ch }
func (nopSubscription) Done() <-chan struct{}          { ch := make(chan struct{}); return ch }
func (nopSubscription) Close()                         {}

func TestBlastRadiusHook1_HRAEscalatesAtHigh(t *testing.T) {
	log := &n4FakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "sess-n4", "proj-n4")

	for i := 0; i < 3; i++ {
		rules.AckedAtLayer(hra.LayerStrategic)
	}

	before, _, _ := rules.L3Counters()
	rules.HighBlastRadius(context.Background(), "high", 0.85, []string{"pkg.A", "pkg.B"})
	after, _, _ := rules.L3Counters()

	if after != before+1 {
		t.Errorf("L3 escalations = %d; want %d (high blast-radius bumps the strategic counter)", after, before+1)
	}

	var evts []eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtEscalationDecision {
			evts = append(evts, ev)
		}
	}
	if len(evts) != 1 {
		t.Fatalf("emitted %d escalation events; want 1", len(evts))
	}
	if from := evts[0].Payload["from"]; from != "strategic" {
		t.Errorf("escalation from = %v; want strategic (L3 — HOOK 1 routes through LayerStrategic)", from)
	}
	if nf := evts[0].Payload["needs_fix"]; nf != true {
		t.Errorf("needs_fix = %v; want true (HOOK 1 sets NeedsFix=true on the strategic finding)", nf)
	}

	logLow := &n4FakeEventLog{}
	rulesLow := hra.NewEscalationRules(logLow, fc, "sess-n4-low", "proj-n4-low")
	prevEsc, _, _ := rulesLow.L3Counters()
	rulesLow.HighBlastRadius(context.Background(), "medium", 0.5, []string{"pkg.A"})
	rulesLow.HighBlastRadius(context.Background(), "low", 0.1, nil)
	newEsc, _, _ := rulesLow.L3Counters()
	if newEsc != prevEsc {
		t.Errorf("L3 escalations bumped on non-high verdict (%d→%d); hook must NOT fire below high", prevEsc, newEsc)
	}
	for _, ev := range logLow.Appends() {
		if ev.Type == eventlog.EvtEscalationDecision {
			t.Errorf("EvtEscalationDecision emitted on non-high verdict; want no event (bite-check)")
		}
	}
}

func TestBlastRadiusHook2_ConfirmationPolicyPauses(t *testing.T) {
	pol := orchestrator.NewConfirmationPolicy(
		map[orchestrator.DecisionClass]orchestrator.Threshold{
			orchestrator.DecisionHighBlastRadius: orchestrator.ThresholdHigh,
		},
		false,
	)
	got := pol.Evaluate(orchestrator.DecisionHighBlastRadius, orchestrator.DecisionEvent{
		Class: orchestrator.DecisionHighBlastRadius,
	})
	if got != orchestrator.ConfirmationActionMandatoryPause {
		t.Errorf("Evaluate(DecisionHighBlastRadius@ThresholdHigh) = %v; want MandatoryPause (inv-zen-235 HOOK 2)", got)
	}

	polEmpty := orchestrator.NewConfirmationPolicy(
		map[orchestrator.DecisionClass]orchestrator.Threshold{},
		false,
	)
	if polEmpty.Evaluate(orchestrator.DecisionHighBlastRadius, orchestrator.DecisionEvent{
		Class: orchestrator.DecisionHighBlastRadius,
	}) != orchestrator.ConfirmationActionMandatoryPause {
		t.Errorf("unmapped DecisionHighBlastRadius did not default to MandatoryPause (defense-in-depth)")
	}

	polLow := orchestrator.NewConfirmationPolicy(
		map[orchestrator.DecisionClass]orchestrator.Threshold{
			orchestrator.DecisionHighBlastRadius: orchestrator.ThresholdLow,
		},
		false,
	)
	if polLow.Evaluate(orchestrator.DecisionHighBlastRadius, orchestrator.DecisionEvent{
		Class: orchestrator.DecisionHighBlastRadius,
	}) != orchestrator.ConfirmationActionContinue {
		t.Errorf("Evaluate(DecisionHighBlastRadius@ThresholdLow) should be Continue (bite-check)")
	}
}

func TestBlastRadiusHook3_ModeEscalatesToStricter(t *testing.T) {
	for _, base := range merge.AllModes() {
		got := merge.EscalateForBlastRadius(base, "high")
		if got != merge.ModeHighRisk {
			t.Errorf("EscalateForBlastRadius(%v, \"high\") = %v; want ModeHighRisk (spec §9.2 row 3 — override regardless of cost pressure)", base, got)
		}
	}

	for _, base := range merge.AllModes() {
		if base == merge.ModeHighRisk {
			continue
		}
		got := merge.EscalateForBlastRadius(base, "low")
		if got != base {
			t.Errorf("EscalateForBlastRadius(%v, \"low\") = %v; want base unchanged (bite-check)", base, got)
		}
		got = merge.EscalateForBlastRadius(base, "medium")
		if got != base {
			t.Errorf("EscalateForBlastRadius(%v, \"medium\") = %v; want base unchanged (bite-check)", base, got)
		}
	}
}

func TestBlastRadiusHook4_MergeAddsPenalty(t *testing.T) {
	scorer := merge.NewScorer()
	ctx := context.Background()

	candidates := []merge.CandidateOutcome{
		{
			Candidate:     merge.MergeCandidate{HeadSHA: "high-blast", Branch: "b-high"},
			TestPassCount: 10,
			BlastRadius:   0.9,
		},
		{
			Candidate:     merge.MergeCandidate{HeadSHA: "low-blast", Branch: "b-low"},
			TestPassCount: 10,
			BlastRadius:   0.1,
		},
	}
	cfg := merge.ScoringConfig{
		DeltaBlastRadiusPenalty: 1.0,
	}

	res, err := scorer.Rank(ctx, candidates, nil, cfg)
	if err != nil {
		t.Fatalf("Rank returned error: %v", err)
	}
	if res.WinnerID != "low-blast" {
		t.Errorf("Rank winner = %q; want %q (low blast-radius candidate should win — −δ·BlastRadius penalty)", res.WinnerID, "low-blast")
	}

	cfgZeroDelta := merge.ScoringConfig{DeltaBlastRadiusPenalty: 0}
	resZero, errZero := scorer.Rank(ctx, candidates, nil, cfgZeroDelta)
	if errZero != nil {
		t.Fatalf("Rank (zero-delta) returned error: %v", errZero)
	}
	if resZero.WinnerID != "high-blast" {
		t.Errorf("Rank (delta=0) winner = %q; want %q (lex-by-HeadSHA when delta is zero — bite-check)", resZero.WinnerID, "high-blast")
	}
}

func TestF7CompositeCarriesRealData(t *testing.T) {
	tmp := t.TempDir()
	fixtureDir := writeGoFixtureProject(t, tmp)

	h := startDaemonWithProject(t, fixtureDir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = ctx

	qRes := callToolRaw(t, h, "mcp_zen-swarm_caronte_query", map[string]any{
		"query": "fixproj.go",
	})
	if qRes.rpcErr != "" {
		t.Errorf("F7 caronte_query returned JSON-RPC error: %s", qRes.rpcErr)
	}

	iRes := callToolRaw(t, h, "mcp_zen-swarm_caronte_impact", map[string]any{
		"symbol": "fixproj.go",
	})
	if iRes.rpcErr != "" {
		t.Errorf("F7 caronte_impact returned JSON-RPC error: %s", iRes.rpcErr)
	}

	cRes := callToolRaw(t, h, "mcp_zen-swarm_caronte_context", map[string]any{
		"symbol": "fixproj.go",
	})
	if cRes.rpcErr != "" {
		t.Errorf("F7 caronte_context returned JSON-RPC error: %s", cRes.rpcErr)
	}

	ccRes := callToolRaw(t, h, "mcp_zen-swarm_caronte_get_cochange", map[string]any{
		"file": "fixproj.go",
	})
	if ccRes.rpcErr != "" {
		t.Errorf("F7 caronte_get_cochange returned JSON-RPC error: %s", ccRes.rpcErr)
	}

	t.Logf("F7 composite OK: query-payload-keys=%v impact-payload-keys=%v context-payload-keys=%v cochange-payload-keys=%v",
		keysOf(qRes.payload), keysOf(iRes.payload), keysOf(cRes.payload), keysOf(ccRes.payload))
}
