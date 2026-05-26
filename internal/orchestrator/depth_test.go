package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func pow(base, exp int) int {
	r := 1
	for i := 0; i < exp; i++ {
		r *= base
	}
	return r
}

func fakeSpecWithParallelism(t *testing.T, n int) orchestrator.Spec {
	t.Helper()
	return fakeSpec{parallelUpper: n, taskCount: n, phases: 1}
}

func TestDoctrineBoundsForCanonicalMatrix(t *testing.T) {
	cases := []struct {
		name                                  string
		wantFloor, wantMaxWidth, wantMaxDepth int
	}{
		{"max-scope", 8, 32, 5},
		{"default", 3, 12, 3},
		{"capa-firewall", 5, 15, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := orchestrator.DoctrineBoundsFor(tc.name)
			if b.Floor != tc.wantFloor {
				t.Errorf("Floor = %d, want %d", b.Floor, tc.wantFloor)
			}
			if b.MaxWidth != tc.wantMaxWidth {
				t.Errorf("MaxWidth = %d, want %d", b.MaxWidth, tc.wantMaxWidth)
			}
			if b.MaxDepth != tc.wantMaxDepth {
				t.Errorf("MaxDepth = %d, want %d", b.MaxDepth, tc.wantMaxDepth)
			}
		})
	}
}

func TestDoctrineBoundsForUnknownFallsBackToDefault(t *testing.T) {
	unknown := orchestrator.DoctrineBoundsFor("unknown-doctrine-xyz")
	def := orchestrator.DoctrineBoundsFor("default")
	if unknown != def {
		t.Fatalf("DoctrineBoundsFor(unknown) = %+v, want %+v (default fallback)", unknown, def)
	}
}

func TestDoctrineBoundsForEmpty(t *testing.T) {
	empty := orchestrator.DoctrineBoundsFor("")
	def := orchestrator.DoctrineBoundsFor("default")
	if empty != def {
		t.Fatalf("DoctrineBoundsFor(\"\") = %+v, want %+v (default fallback)", empty, def)
	}
}

func TestDoctrineBoundsForKnownDoctrines(t *testing.T) {
	cases := []struct {
		doctrine     string
		wantMaxWidth int
		wantMaxDepth int
	}{
		{"max-scope", 32, 5},
		{"capa-firewall", 15, 4},
		{"default", 12, 3},
		{"unrecognised", 12, 3},
		{"", 12, 3},
	}
	for _, tc := range cases {
		t.Run(tc.doctrine, func(t *testing.T) {
			req := orchestrator.BuildRequest{
				Doctrine: tc.doctrine,
				Spec:     fakeSpec{taskCount: 1024, parallelUpper: 1024},
			}

			width, err := orchestrator.DecideWidth(req, 1024, orchestrator.DoctrineBounds{
				MaxWidth: tc.wantMaxWidth,
				MaxDepth: tc.wantMaxDepth,
			})
			if err != nil {
				t.Fatalf("DecideWidth: %v", err)
			}
			if width != tc.wantMaxWidth {
				t.Fatalf("width = %d, want %d", width, tc.wantMaxWidth)
			}

			n := pow(tc.wantMaxWidth, tc.wantMaxDepth) + 1
			depth, err := orchestrator.DecideDepth(n, width, tc.wantMaxDepth)
			if err != nil {
				t.Fatalf("DecideDepth: %v", err)
			}
			if depth != tc.wantMaxDepth {
				t.Fatalf("depth = %d, want %d (cap on log_W(N))", depth, tc.wantMaxDepth)
			}
		})
	}
}

func TestDecideWidthMaxScopeBoundedByCapacity(t *testing.T) {
	bounds := orchestrator.DoctrineBoundsFor("max-scope")
	req := orchestrator.BuildRequest{Spec: fakeSpecWithParallelism(t, 16)}
	width, err := orchestrator.DecideWidth(req, 4, bounds)
	if err != nil {
		t.Fatalf("DecideWidth: %v", err)
	}
	if width != 4 {
		t.Fatalf("width = %d, want 4 (capacity binds)", width)
	}
}

func TestDecideWidthDefaultBoundedByDoctrine(t *testing.T) {
	bounds := orchestrator.DoctrineBoundsFor("default")
	req := orchestrator.BuildRequest{Spec: fakeSpecWithParallelism(t, 100)}
	width, err := orchestrator.DecideWidth(req, 1000, bounds)
	if err != nil {
		t.Fatalf("DecideWidth: %v", err)
	}
	if width != 12 {
		t.Fatalf("width = %d, want 12 (doctrine MaxWidth binds)", width)
	}
}

func TestDecideWidthBoundedByParallelism(t *testing.T) {
	bounds := orchestrator.DoctrineBoundsFor("max-scope")
	req := orchestrator.BuildRequest{Spec: fakeSpecWithParallelism(t, 2)}
	width, err := orchestrator.DecideWidth(req, 1000, bounds)
	if err != nil {
		t.Fatalf("DecideWidth: %v", err)
	}
	if width != 2 {
		t.Fatalf("width = %d, want 2 (parallelism binds)", width)
	}
}

func TestDecideWidthSpecCapsWidth(t *testing.T) {
	width, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 3}},
		32,
		orchestrator.DoctrineBounds{MaxWidth: 32, MaxDepth: 5},
	)
	if err != nil {
		t.Fatalf("DecideWidth: %v", err)
	}
	if width != 3 {
		t.Fatalf("width = %d, want 3 (spec ParallelizableUpperBound binds)", width)
	}
}

func TestDecideWidthCapacityBinds(t *testing.T) {
	width, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 100}},
		2,
		orchestrator.DoctrineBounds{MaxWidth: 32, MaxDepth: 5},
	)
	if err != nil {
		t.Fatalf("DecideWidth: %v", err)
	}
	if width != 2 {
		t.Fatalf("width = %d, want 2 (capacity binds)", width)
	}
}

func TestDecideWidthZeroParallelismYieldsErr(t *testing.T) {
	_, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 0}},
		8,
		orchestrator.DoctrineBounds{MaxWidth: 32, MaxDepth: 5},
	)
	if !errors.Is(err, orchestrator.ErrZeroWidth) {
		t.Fatalf("DecideWidth(parallelism=0): want ErrZeroWidth, got %v", err)
	}
}

func TestDecideWidthZeroCapacityYieldsErr(t *testing.T) {
	_, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 8}},
		0,
		orchestrator.DoctrineBounds{MaxWidth: 32, MaxDepth: 5},
	)
	if !errors.Is(err, orchestrator.ErrZeroWidth) {
		t.Fatalf("DecideWidth(capacity=0): want ErrZeroWidth, got %v", err)
	}
}

func TestDecideWidthNegativeCapacityYieldsErr(t *testing.T) {
	_, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 8}},
		-1,
		orchestrator.DoctrineBounds{MaxWidth: 32, MaxDepth: 5},
	)
	if !errors.Is(err, orchestrator.ErrZeroWidth) {
		t.Fatalf("DecideWidth(capacity=-1): want ErrZeroWidth, got %v", err)
	}
}

func TestDecideWidthZeroMaxWidthYieldsErr(t *testing.T) {
	_, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 8}},
		8,
		orchestrator.DoctrineBounds{MaxWidth: 0, MaxDepth: 5},
	)
	if !errors.Is(err, orchestrator.ErrZeroWidth) {
		t.Fatalf("DecideWidth(MaxWidth=0): want ErrZeroWidth, got %v", err)
	}
}

func TestDecideWidthNilSpecReturnsInvalidRequest(t *testing.T) {
	_, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{},
		8,
		orchestrator.DoctrineBounds{MaxWidth: 32, MaxDepth: 5},
	)
	if !errors.Is(err, orchestrator.ErrInvalidBuildRequest) {
		t.Fatalf("DecideWidth(nil Spec): want ErrInvalidBuildRequest, got %v", err)
	}
}

func TestDecideWidthRejectsInvalidCapacity(t *testing.T) {

	_, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 4}},
		0,
		orchestrator.DoctrineBounds{MaxWidth: 4, MaxDepth: 2},
	)
	if !errors.Is(err, orchestrator.ErrZeroWidth) {
		t.Fatalf("DecideWidth(capacity=0): want ErrZeroWidth, got %v", err)
	}
	_, err = orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 4}},
		-1,
		orchestrator.DoctrineBounds{MaxWidth: 4, MaxDepth: 2},
	)
	if !errors.Is(err, orchestrator.ErrZeroWidth) {
		t.Fatalf("DecideWidth(capacity=-1): want ErrZeroWidth, got %v", err)
	}
}

func TestDecideWidthRejectsInvalidBounds(t *testing.T) {

	_, err := orchestrator.DecideWidth(
		orchestrator.BuildRequest{Spec: fakeSpec{parallelUpper: 4}},
		8,
		orchestrator.DoctrineBounds{MaxWidth: 0, MaxDepth: 2},
	)
	if !errors.Is(err, orchestrator.ErrZeroWidth) {
		t.Fatalf("DecideWidth(MaxWidth=0): want ErrZeroWidth, got %v", err)
	}
}

func TestDecideDepthRejectsInvalidMaxDepth(t *testing.T) {
	_, err := orchestrator.DecideDepth(10, 4, 0)
	if !errors.Is(err, orchestrator.ErrInvalidConfig) {
		t.Fatalf("DecideDepth(maxDepth=0): want ErrInvalidConfig, got %v", err)
	}
	_, err = orchestrator.DecideDepth(10, 4, -1)
	if !errors.Is(err, orchestrator.ErrInvalidConfig) {
		t.Fatalf("DecideDepth(maxDepth=-1): want ErrInvalidConfig, got %v", err)
	}
}

func TestDecideDepthSingletonTask_D2Canary(t *testing.T) {
	d, err := orchestrator.DecideDepth(1, 4, 8)
	if err != nil {
		t.Fatalf("DecideDepth(n=1): unexpected error %v", err)
	}
	if d != 1 {
		t.Fatalf("depth = %d, want 1 (single task always fits in one supervisor pass)", d)
	}
}

func TestDecideDepthLinearWidth(t *testing.T) {

	d, err := orchestrator.DecideDepth(3, 1, 8)
	if err != nil {
		t.Fatalf("DecideDepth: %v", err)
	}
	if d != 3 {
		t.Fatalf("depth = %d, want 3 (linear cascade)", d)
	}

	d, err = orchestrator.DecideDepth(20, 1, 5)
	if err != nil {
		t.Fatalf("DecideDepth: %v", err)
	}
	if d != 5 {
		t.Fatalf("depth = %d, want 5 (cap)", d)
	}
}

func TestDecideDepthCeilLogW(t *testing.T) {

	d, err := orchestrator.DecideDepth(16, 4, 8)
	if err != nil {
		t.Fatalf("DecideDepth: %v", err)
	}
	if d != 2 {
		t.Fatalf("depth = %d, want 2", d)
	}

	d, err = orchestrator.DecideDepth(17, 4, 8)
	if err != nil {
		t.Fatalf("DecideDepth: %v", err)
	}
	if d != 3 {
		t.Fatalf("depth = %d, want 3", d)
	}
}

func TestDecideDepthMaxCap(t *testing.T) {

	d, err := orchestrator.DecideDepth(1024, 2, 4)
	if err != nil {
		t.Fatalf("DecideDepth: %v", err)
	}
	if d != 4 {
		t.Fatalf("depth = %d, want 4 (cap)", d)
	}
}

func TestDecideDepthLogWN(t *testing.T) {
	cases := []struct {
		name     string
		n, w, md int
		want     int
	}{
		{"N64_W8", 64, 8, 8, 2},
		{"N100_W8", 100, 8, 8, 3},
		{"N8_W8", 8, 8, 8, 1},
		{"N1_W1", 1, 1, 5, 1},
		{"N10000_W2_capped_at_3", 10000, 2, 3, 3},
		{"N50_W1_sequential_cap5", 50, 1, 5, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := orchestrator.DecideDepth(tc.n, tc.w, tc.md)
			if err != nil {
				t.Fatalf("DecideDepth(%d,%d,%d): unexpected error %v", tc.n, tc.w, tc.md, err)
			}
			if d != tc.want {
				t.Fatalf("DecideDepth(%d,%d,%d) = %d, want %d", tc.n, tc.w, tc.md, d, tc.want)
			}
		})
	}
}

func TestDecideDepthZeroTasks(t *testing.T) {
	_, err := orchestrator.DecideDepth(0, 4, 8)
	if !errors.Is(err, orchestrator.ErrZeroDepth) {
		t.Fatalf("DecideDepth(n=0): want ErrZeroDepth, got %v", err)
	}
}

func TestDecideDepthNegativeTasks(t *testing.T) {
	_, err := orchestrator.DecideDepth(-5, 4, 5)
	if !errors.Is(err, orchestrator.ErrZeroDepth) {
		t.Fatalf("DecideDepth(-5): want ErrZeroDepth, got %v", err)
	}
}

func TestDecideDepthInvalidWidth(t *testing.T) {
	_, err := orchestrator.DecideDepth(10, 0, 5)
	if !errors.Is(err, orchestrator.ErrInvalidBuildRequest) {
		t.Fatalf("DecideDepth(w=0): want ErrInvalidBuildRequest, got %v", err)
	}
}

func TestDecideDepthNegativeWidth(t *testing.T) {
	_, err := orchestrator.DecideDepth(10, -1, 5)
	if !errors.Is(err, orchestrator.ErrInvalidBuildRequest) {
		t.Fatalf("DecideDepth(w=-1): want ErrInvalidBuildRequest, got %v", err)
	}
}

func TestDecideDepthInvalidMaxDepth(t *testing.T) {
	cases := []struct{ md int }{{0}, {-1}}
	for _, tc := range cases {
		_, err := orchestrator.DecideDepth(10, 4, tc.md)
		if !errors.Is(err, orchestrator.ErrInvalidConfig) {
			t.Fatalf("DecideDepth(maxDepth=%d): want ErrInvalidConfig, got %v", tc.md, err)
		}
	}
}

func TestDecideDepthSingleTaskAnyWidth(t *testing.T) {
	for _, w := range []int{1, 4, 32} {
		d, err := orchestrator.DecideDepth(1, w, 8)
		if err != nil {
			t.Fatalf("DecideDepth(n=1, w=%d): unexpected error %v", w, err)
		}
		if d != 1 {
			t.Fatalf("DecideDepth(n=1, w=%d) = %d, want 1", w, d)
		}
	}
}

func TestDecideDepthW1Sequential(t *testing.T) {
	d, err := orchestrator.DecideDepth(3, 1, 5)
	if err != nil {
		t.Fatalf("DecideDepth(3,1,5): %v", err)
	}
	if d != 3 {
		t.Fatalf("depth = %d, want 3 (sequential uncapped)", d)
	}
	d, err = orchestrator.DecideDepth(10, 1, 5)
	if err != nil {
		t.Fatalf("DecideDepth(10,1,5): %v", err)
	}
	if d != 5 {
		t.Fatalf("depth = %d, want 5 (sequential capped at maxDepth)", d)
	}
}

func TestDecideDepthExactPower(t *testing.T) {

	d, err := orchestrator.DecideDepth(64, 4, 8)
	if err != nil {
		t.Fatalf("DecideDepth(64,4,8): %v", err)
	}
	if d != 3 {
		t.Fatalf("depth = %d, want 3 (exact power N=W^depth, precision check)", d)
	}
}

func TestDecideDepthLargeN(t *testing.T) {
	d, err := orchestrator.DecideDepth(1_000_000, 10, 8)
	if err != nil {
		t.Fatalf("DecideDepth(1_000_000,10,8): %v", err)
	}
	if d != 6 {
		t.Fatalf("depth = %d, want 6 (uncapped log_10(1e6)=6)", d)
	}
	d, err = orchestrator.DecideDepth(1_000_000, 10, 4)
	if err != nil {
		t.Fatalf("DecideDepth(1_000_000,10,4): %v", err)
	}
	if d != 4 {
		t.Fatalf("depth = %d, want 4 (capped at maxDepth)", d)
	}
}

func newTestMemLog(t *testing.T) *eventlog.Log {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC))
	return eventlog.NewMemory(clk)
}

func appendEvent(t *testing.T, log *eventlog.Log, et eventlog.EventType, sessionID, projectID string) {
	t.Helper()
	var payload map[string]any
	switch et {
	case eventlog.EvtResearchCompleted:
		payload = map[string]any{"findings_summary": "ok", "cost_usd": 0.0}
	case eventlog.EvtOrchestratorStarted:
		payload = map[string]any{"session_id": sessionID, "project_id": projectID, "autonomy_mode": "autonomous"}
	case eventlog.EvtDepthWidthDecided:
		payload = map[string]any{"depth": 2, "width": 4, "rationale": "test"}
	default:
		payload = map[string]any{}
	}
	ev := eventlog.Event{
		Type:      et,
		SessionID: sessionID,
		ProjectID: projectID,
		Payload:   payload,
	}
	if _, err := log.Append(context.Background(), ev); err != nil {
		t.Fatalf("appendEvent(%s): %v", et, err)
	}
}

func TestResearchGateNewNilQuerier(t *testing.T) {
	_, err := orchestrator.NewResearchGate(nil)
	if !errors.Is(err, orchestrator.ErrInvalidConfig) {
		t.Fatalf("NewResearchGate(nil): want ErrInvalidConfig wrap, got %v", err)
	}
}

func TestResearchGatePassesWhenEventPresent(t *testing.T) {
	memLog := newTestMemLog(t)
	appendEvent(t, memLog, eventlog.EvtResearchCompleted, "sess-D6-pass", "proj-D6")

	gate, err := orchestrator.NewResearchGate(memLog)
	if err != nil {
		t.Fatalf("NewResearchGate: %v", err)
	}
	if err := gate.Check(context.Background(), "sess-D6-pass"); err != nil {
		t.Fatalf("Check: want nil, got %v", err)
	}
}

func TestResearchGateFailsWhenAbsent(t *testing.T) {
	memLog := newTestMemLog(t)

	gate, err := orchestrator.NewResearchGate(memLog)
	if err != nil {
		t.Fatalf("NewResearchGate: %v", err)
	}
	err = gate.Check(context.Background(), "sess-D6-absent")
	if !errors.Is(err, orchestrator.ErrResearchGateNotPassed) {
		t.Fatalf("Check on empty log: want ErrResearchGateNotPassed, got %v", err)
	}
}

func TestResearchGateIgnoresOtherSessions(t *testing.T) {
	memLog := newTestMemLog(t)

	appendEvent(t, memLog, eventlog.EvtResearchCompleted, "OTHER-session", "proj-D6")

	gate, err := orchestrator.NewResearchGate(memLog)
	if err != nil {
		t.Fatalf("NewResearchGate: %v", err)
	}
	err = gate.Check(context.Background(), "MINE-session")
	if !errors.Is(err, orchestrator.ErrResearchGateNotPassed) {
		t.Fatalf("Check(MINE) with event only for OTHER: want ErrResearchGateNotPassed, got %v", err)
	}
}

// TestResearchGateIgnoresOtherEventTypes asserts that events other than
// EvtResearchCompleted do not satisfy the gate (e.g. EvtOrchestratorStarted).
func TestResearchGateIgnoresOtherEventTypes(t *testing.T) {
	memLog := newTestMemLog(t)
	appendEvent(t, memLog, eventlog.EvtOrchestratorStarted, "sess-D6-wrongtype", "proj-D6")

	gate, err := orchestrator.NewResearchGate(memLog)
	if err != nil {
		t.Fatalf("NewResearchGate: %v", err)
	}
	err = gate.Check(context.Background(), "sess-D6-wrongtype")
	if !errors.Is(err, orchestrator.ErrResearchGateNotPassed) {
		t.Fatalf("Check with only OrchestratorStarted: want ErrResearchGateNotPassed, got %v", err)
	}
}

func TestResearchGateMultipleEventsFirstHitWins(t *testing.T) {
	memLog := newTestMemLog(t)
	const sess = "sess-D6-multi"
	const proj = "proj-D6"
	appendEvent(t, memLog, eventlog.EvtOrchestratorStarted, sess, proj)
	appendEvent(t, memLog, eventlog.EvtResearchCompleted, sess, proj)
	appendEvent(t, memLog, eventlog.EvtDepthWidthDecided, sess, proj)

	gate, err := orchestrator.NewResearchGate(memLog)
	if err != nil {
		t.Fatalf("NewResearchGate: %v", err)
	}
	if err := gate.Check(context.Background(), sess); err != nil {
		t.Fatalf("Check with multiple events incl. ResearchCompleted: want nil, got %v", err)
	}
}

func TestResearchGateCtxCancellation(t *testing.T) {
	memLog := newTestMemLog(t)

	gate, err := orchestrator.NewResearchGate(memLog)
	if err != nil {
		t.Fatalf("NewResearchGate: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = gate.Check(ctx, "sess-D6-ctx")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Check with cancelled ctx: want context.Canceled chain, got %v", err)
	}
}

type fakeQuerier struct {
	records []eventlog.Record
	err     error
}

func (f *fakeQuerier) Query(_ context.Context, _ string, _ int64) ([]eventlog.Record, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

func TestResearchGateQueryError(t *testing.T) {
	queryErr := errors.New("eventlog backend unavailable")
	gate, err := orchestrator.NewResearchGate(&fakeQuerier{err: queryErr})
	if err != nil {
		t.Fatalf("NewResearchGate: %v", err)
	}
	err = gate.Check(context.Background(), "sess-D6-queryerr")
	if !errors.Is(err, queryErr) {
		t.Fatalf("Check on query error: want queryErr in chain, got %v", err)
	}
}

func TestRunStage4WithCanonicalResearchGate(t *testing.T) {
	t.Run("gatePass", func(t *testing.T) {
		clk := clock.NewFake(time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC))
		memLog := eventlog.NewMemory(clk)

		appendEvent(t, memLog, eventlog.EvtResearchCompleted, testSessionID, testProjectID)

		gate, err := orchestrator.NewResearchGate(memLog)
		if err != nil {
			t.Fatalf("NewResearchGate: %v", err)
		}
		sm := orchestrator.NewStateMachine(memLog, clk, testSessionID, testProjectID)
		pool := &fakePool{}
		disp := &fakeDispatcher{}

		orch, err := orchestrator.New(orchestrator.Config{
			Clock:        clk,
			EventLog:     memLog,
			StateMachine: sm,
			Pool:         pool,
			Dispatcher:   disp,
			Research:     gate,
			SessionID:    testSessionID,
			ProjectID:    testProjectID,
			PoolCapacity: 8,
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := orch.Init(context.Background()); err != nil {
			t.Fatalf("Init: %v", err)
		}
		req := orchestrator.BuildRequest{
			SessionID: testSessionID,
			ProjectID: testProjectID,
			Doctrine:  "max-scope",
			Spec:      defaultSpec(),
			Autonomy:  "autonomous",
		}
		if err := orch.RunStage4(context.Background(), req); err != nil {
			t.Fatalf("RunStage4 with canonical gate (seeded): %v", err)
		}
		if orch.State() != orchestrator.StateIdle {
			t.Fatalf("end state = %v, want Idle", orch.State())
		}
	})

	t.Run("gateFail", func(t *testing.T) {
		clk := clock.NewFake(time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC))
		memLog := eventlog.NewMemory(clk)

		gate, err := orchestrator.NewResearchGate(memLog)
		if err != nil {
			t.Fatalf("NewResearchGate: %v", err)
		}
		sm := orchestrator.NewStateMachine(memLog, clk, testSessionID, testProjectID)
		pool := &fakePool{}
		disp := &fakeDispatcher{}

		orch, err := orchestrator.New(orchestrator.Config{
			Clock:        clk,
			EventLog:     memLog,
			StateMachine: sm,
			Pool:         pool,
			Dispatcher:   disp,
			Research:     gate,
			SessionID:    testSessionID,
			ProjectID:    testProjectID,
			PoolCapacity: 8,
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := orch.Init(context.Background()); err != nil {
			t.Fatalf("Init: %v", err)
		}
		req := orchestrator.BuildRequest{
			SessionID: testSessionID,
			ProjectID: testProjectID,
			Doctrine:  "max-scope",
			Spec:      defaultSpec(),
			Autonomy:  "autonomous",
		}
		err = orch.RunStage4(context.Background(), req)
		if !errors.Is(err, orchestrator.ErrResearchGateNotPassed) {
			t.Fatalf("RunStage4 without research event: want ErrResearchGateNotPassed, got %v", err)
		}
		if orch.State() != orchestrator.StateIdle {
			t.Fatalf("end state = %v, want Idle (graceful unwind)", orch.State())
		}
		if disp.dispatchCalls != 0 {
			t.Fatalf("dispatch must not run when gate blocks, got %d calls", disp.dispatchCalls)
		}
	})
}
