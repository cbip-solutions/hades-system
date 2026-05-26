package orchestrator_test

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fakeSpec struct {
	phases        int
	taskCount     int
	parallelUpper int
	dependencyDAG any
}

func (s fakeSpec) Phases() int                   { return s.phases }
func (s fakeSpec) TaskCount() int                { return s.taskCount }
func (s fakeSpec) ParallelizableUpperBound() int { return s.parallelUpper }
func (s fakeSpec) DependencyDAG() any            { return s.dependencyDAG }

func defaultSpec() fakeSpec {
	return fakeSpec{phases: 3, taskCount: 7, parallelUpper: 4}
}

type harness struct {
	t       *testing.T
	clk     *clock.Fake
	memLog  *eventlog.Log
	gate    *fakeGate
	disp    *fakeDispatcher
	pool    *fakePool
	confirm func(ctx context.Context, ev orchestrator.DispatchDecisionEvent) error
	orch    *orchestrator.Orchestrator
	cfg     orchestrator.Config
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	clk := newTestClock()
	memLog := eventlog.NewMemory(clk)
	sm := orchestrator.NewStateMachine(memLog, clk, testSessionID, testProjectID)
	pool := &fakePool{}
	disp := &fakeDispatcher{}
	gate := &fakeGate{}

	cfg := orchestrator.Config{
		Clock:        clk,
		EventLog:     memLog,
		StateMachine: sm,
		Pool:         pool,
		Dispatcher:   disp,
		Research:     gate,
		SessionID:    testSessionID,
		ProjectID:    testProjectID,
		PoolCapacity: 8,
	}
	orch, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatalf("orchestrator.New: %v", err)
	}
	return &harness{
		t:      t,
		clk:    clk,
		memLog: memLog,
		gate:   gate,
		disp:   disp,
		pool:   pool,
		orch:   orch,
		cfg:    cfg,
	}
}

func (h *harness) init() {
	h.t.Helper()
	if err := h.orch.Init(context.Background()); err != nil {
		h.t.Fatalf("Init: %v", err)
	}
}

func (h *harness) seedResearchCompleted() {
	h.t.Helper()
	ev := eventlog.Event{
		Type:      eventlog.EvtResearchCompleted,
		SessionID: testSessionID,
		ProjectID: testProjectID,
		Payload: map[string]any{
			"findings_summary": "ok",
			"cost_usd":         0.0,
		},
	}
	if _, err := h.memLog.Append(context.Background(), ev); err != nil {
		h.t.Fatalf("seed EvtResearchCompleted: %v", err)
	}
}

func (h *harness) build(spec orchestrator.Spec, doctrine string) orchestrator.BuildRequest {
	return orchestrator.BuildRequest{
		SessionID:            testSessionID,
		ProjectID:            testProjectID,
		Doctrine:             doctrine,
		Spec:                 spec,
		Autonomy:             "autonomous",
		ConfirmationCallback: h.confirm,
	}
}

func (h *harness) records() []eventlog.Record {
	h.t.Helper()
	recs, err := h.memLog.Query(context.Background(), testSessionID, 0)
	if err != nil {
		h.t.Fatalf("memLog.Query: %v", err)
	}
	return recs
}

func (h *harness) assertEventOrder(want ...eventlog.EventType) {
	h.t.Helper()
	recs := h.records()
	idx := 0
	for _, r := range recs {
		if idx >= len(want) {
			break
		}
		if r.EventType == want[idx] {
			idx++
		}
	}
	if idx != len(want) {

		actual := make([]string, 0, len(recs))
		for _, r := range recs {
			actual = append(actual, r.EventType.String())
		}
		h.t.Fatalf("event ordering: missed %s at position %d; actual=%v",
			want[idx], idx, actual)
	}
}

func (h *harness) assertOrchestratorStopped(wantOutcome string) {
	h.t.Helper()
	recs := h.records()
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].EventType != eventlog.EvtOrchestratorStopped {
			continue
		}
		decoded, err := eventlog.Decode(recs[i].EventType, recs[i].Payload)
		if err != nil {
			h.t.Fatalf("decode OrchestratorStopped payload: %v", err)
		}
		stopped, ok := decoded.(eventlog.OrchestratorStopped)
		if !ok {
			h.t.Fatalf("decoded %T, want eventlog.OrchestratorStopped", decoded)
		}
		if stopped.Outcome != wantOutcome {
			h.t.Fatalf("OrchestratorStopped.Outcome = %q, want %q", stopped.Outcome, wantOutcome)
		}
		return
	}
	h.t.Fatalf("no EvtOrchestratorStopped row in event log")
}
